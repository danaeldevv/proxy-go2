package main

import (
	"bufio"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	logFilePath = "/var/log/proxyws.log"
	pidFileDir  = "/var/run"
	serviceDir  = "/etc/systemd/system"
	readTimeout = time.Second // Timeout para leitura inicial
)

var (
	logMutex  sync.Mutex
	sslConfig *tls.Config
)

func logMessage(msg string) {
	logMutex.Lock()
	defer logMutex.Unlock()
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Erro ao escrever no log: %v\n", err)
		return
	}
	defer f.Close()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(f, "[%s] %s\n", timestamp, msg)
}

type peekConn struct {
	net.Conn
	peeked []byte
}

func (p *peekConn) Read(b []byte) (int, error) {
	if len(p.peeked) > 0 {
		n := copy(b, p.peeked)
		p.peeked = p.peeked[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}

func readInitialBytes(conn net.Conn, n int) ([]byte, error) {
	buf := make([]byte, n)
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	readBytes := 0
	for readBytes < n {
		rn, err := conn.Read(buf[readBytes:])
		if err != nil {
			return buf[:readBytes], err
		}
		readBytes += rn
	}
	conn.SetReadDeadline(time.Time{})
	return buf, nil
}

func readInitialData(conn net.Conn) (string, error) {
	buf := make([]byte, 8192)
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	conn.SetReadDeadline(time.Time{})
	return string(buf[:n]), nil
}

func computeAcceptKey(secWebSocketKey string) string {
	const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(secWebSocketKey + magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func handleWebSocketHandshake(conn net.Conn, request string) error {
	lines := strings.Split(request, "\r\n")
	var secWebSocketKey string
	for _, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-key:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				secWebSocketKey = strings.TrimSpace(parts[1])
				break
			}
		}
	}
	if secWebSocketKey == "" {
		return fmt.Errorf("Sec-WebSocket-Key não encontrado")
	}
	acceptKey := computeAcceptKey(secWebSocketKey)
	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + acceptKey + "\r\n\r\n"

	_, err := conn.Write([]byte(response))
	return err
}

func tryProtocols(conn net.Conn) {
	defer conn.Close()

	// Primeiro lê os 1-3 bytes iniciais para detectar se é TLS ou SOCKS
	peekBytes, err := peekInitialBytes(conn, 3)
	if err != nil {
		logMessage("Erro ao fazer peek nos bytes iniciais: " + err.Error())
		return
	}

	if len(peekBytes) == 0 {
		logMessage("Nenhum dado recebido para determinar protocolo")
		return
	}

	// Se 1o byte é 0x05 = SOCKS5
	if peekBytes[0] == 0x05 {
		if trySocks(conn) {
			return
		}
	}

	// Se 1o byte 0x16 e possui sslConfig = TLS ClientHello -> WebSocket Secure
	if peekBytes[0] == 0x16 && sslConfig != nil {
		if tryWebSocket(conn, true) {
			return
		}
	}

	// Caso contrário, tenta WebSocket normal
	if tryWebSocket(conn, false) {
		return
	}

	// Fallback TCP simples
	tryTCP(conn)
}

// Faz peek (leitura sem consumir) dos primeiros n bytes, usando PeekConn temporário
func peekInitialBytes(conn net.Conn, n int) ([]byte, error) {
	// Use bufio.Reader and Peek without consuming data in the main conn
	reader := bufio.NewReader(conn)
	peeked, err := reader.Peek(n)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return peeked, nil
}

// Overwrite tryWebSocket para receber conn agora bufio.Reader disponível
func tryWebSocket(conn net.Conn, useTLS bool) bool {
	// Usar bufio.Reader para suportar peek correto
	bufReader := bufio.NewReader(conn)
	// Ler primeiro request
	reqLines := []string{}
	for {
		line, err := bufReader.ReadString('\n')
		if err != nil {
			logMessage(fmt.Sprintf("Erro lendo linha em WebSocket (TLS=%v): %v", useTLS, err))
			return false
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		reqLines = append(reqLines, line)
	}

	if len(reqLines) == 0 {
		return false
	}

	// Reconstruir o request para envio ao handshake
	request := strings.Join(reqLines, "\r\n") + "\r\n\r\n"

	if useTLS {
		if sslConfig == nil {
			logMessage("SSL Config não definida, não pode usar TLS para WebSocket")
			return false
		}
		tlsConn := tls.Server(&connWithBuf{Conn: conn, reader: bufReader}, sslConfig)
		if err := tlsConn.Handshake(); err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS para WebSocket: %v", err))
			return false
		}
		conn = tlsConn
	} else {
		conn = &connWithBuf{Conn: conn, reader: bufReader}
	}

	if strings.HasPrefix(reqLines[0], "GET") || strings.HasPrefix(reqLines[0], "CONNECT") {
		err := handleWebSocketHandshake(conn, request)
		if err != nil {
			logMessage("Erro no handshake WebSocket: " + err.Error())
			return false
		}
		logMessage(fmt.Sprintf("Conexão WebSocket estabelecida (TLS=%v) e redirecionando para SSH", useTLS))
		sshRedirect(conn)
		return true
	}
	return false
}

type connWithBuf struct {
	net.Conn
	reader *bufio.Reader
}

func (c *connWithBuf) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

func trySocks(conn net.Conn) bool {
	initialData, err := readInitialData(conn)
	if err != nil {
		logMessage(fmt.Sprintf("Erro na leitura inicial para SOCKS5: %v", err))
		return false
	}

	if len(initialData) > 0 && initialData[0] == 0x05 {
		resp := "HTTP/1.1 200 Switching Protocols\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 Switching Protocols para SOCKS5: " + err.Error())
			return false
		}
		logMessage("Conexão SOCKS5 estabelecida, redirecionando para SSH")
		sshRedirect(conn)
		return true
	}
	return false
}

func tryTCP(conn net.Conn) {
	logMessage("Tentativa de conexão TCP simples, redirecionando para SSH")
	resp := "HTTP/1.1 200 OK\r\n\r\n"
	_, _ = conn.Write([]byte(resp))
	sshRedirect(conn)
}

func sshRedirect(clientConn net.Conn) {
	serverConn, err := net.Dial("tcp", "127.0.0.1:22")
	if err != nil {
		logMessage(fmt.Sprintf("Erro ao conectar ao servidor SSH: %v", err))
		return
	}
	defer serverConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(serverConn, clientConn)
	}()
	go func() {
		defer wg.Done()
		io.Copy(clientConn, serverConn)
	}()
	wg.Wait()

	logMessage("Conexão proxy finalizada para SSH")
}

func systemdServicePath(port int) string {
	return fmt.Sprintf("%s/proxyws@%d.service", serviceDir, port)
}

func createSystemdService(port int, execPath string) error {
	serviceContent := fmt.Sprintf(`[Unit]
Description=ProxyWS na porta %d
After=network.target

[Service]
Type=simple
ExecStart=%s %d
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, port, execPath, port)
	path := systemdServicePath(port)
	return os.WriteFile(path, []byte(serviceContent), 0644)
}

func enableAndStartService(port int) error {
	serviceName := fmt.Sprintf("proxyws@%d.service", port)
	cmd := exec.Command("systemctl", "daemon-reload")
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.Command("systemctl", "enable", serviceName)
	if err := cmd.Run(); err != nil {
		return err
	}
	cmd = exec.Command("systemctl", "start", serviceName)
	return cmd.Run()
}

func stopAndDisableService(port int) error {
	serviceName := fmt.Sprintf("proxyws@%d.service", port)
	cmd := exec.Command("systemctl", "stop", serviceName)
	cmd.Run()
	cmd = exec.Command("systemctl", "disable", serviceName)
	cmd.Run()
	return os.Remove(systemdServicePath(port))
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func main() {
	if len(os.Args) > 1 {
		port, err := strconv.Atoi(os.Args[1])
		if err != nil {
			fmt.Printf("Parâmetro inválido: %s\n", os.Args[1])
			return
		}
		cert, err := tls.LoadX509KeyPair("cert.pem", "key.pem")
		if err == nil {
			sslConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		} else {
			logMessage("Certificados TLS não carregados, executando sem TLS")
		}
		startProxy(port)
		return
	}

	execPath, _ := os.Executable()
	scanner := bufio.NewScanner(os.Stdin)

	for {
		clearScreen()
		fmt.Println("============================")
		fmt.Println("      Proxy Cloud JF")
		fmt.Println("============================")
		fmt.Println("== 1 - Abrir nova porta    ==")
		fmt.Println("== 2 - Fechar porta        ==")
		fmt.Println("== 3 - Sair do menu        ==")
		fmt.Println("============================")
		fmt.Print("Escolha uma opção: ")

		if !scanner.Scan() {
			break
		}
		option := scanner.Text()

		switch option {
		case "1":
			clearScreen()
			fmt.Print("Digite a porta para abrir: ")
			if !scanner.Scan() {
				break
			}
			portStr := scanner.Text()
			port, err := strconv.Atoi(portStr)
			if err != nil || port < 1 || port > 65535 {
				fmt.Println("Porta inválida! Pressione Enter...")
				scanner.Scan()
				continue
			}
			if err := createSystemdService(port, execPath); err != nil {
				fmt.Println("Erro criando service: ", err)
				fmt.Print("Pressione Enter...")
				scanner.Scan()
				continue
			}
			if err := enableAndStartService(port); err != nil {
				fmt.Println("Erro ao iniciar service via systemctl: ", err)
				fmt.Print("Pressione Enter...")
				scanner.Scan()
				continue
			}
			fmt.Printf("✅ Proxy iniciado na porta %d\n", port)
			fmt.Println("Executando em background via systemd. Pressione Enter...")
			scanner.Scan()
		case "2":
			clearScreen()
			fmt.Print("Digite a porta a ser fechada: ")
			if !scanner.Scan() {
				break
			}
			portStr := scanner.Text()
			port, err := strconv.Atoi(portStr)
			if err != nil || port < 1 || port > 65535 {
				fmt.Println("Porta inválida! Pressione Enter...")
				scanner.Scan()
				continue
			}
			fmt.Printf("Tem certeza que deseja encerrar a porta %d? (s/n): ", port)
			if !scanner.Scan() {
				break
			}
			conf := strings.ToLower(scanner.Text())
			if conf == "s" {
				if err := stopAndDisableService(port); err != nil {
					fmt.Println("Erro ao parar service: ", err)
				} else {
					fmt.Printf("✅ Porta %d encerrada.\n", port)
				}
			} else {
				fmt.Println("❌ Cancelado.")
			}
			fmt.Print("Pressione Enter...")
			scanner.Scan()
		case "3":
			clearScreen()
			fmt.Println("👋 Saindo do menu. Os proxies ativos continuam em execução.")
			return
		default:
			fmt.Println("❌ Opção inválida! Pressione Enter...")
			scanner.Scan()
		}
	}
}

func startProxy(port int) {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logMessage(fmt.Sprintf("Erro iniciando listener na porta %d: %v", port, err))
		return
	}
	defer listener.Close()

	pidFile := fmt.Sprintf("%s/proxyws_%d.pid", pidFileDir, port)
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		logMessage(fmt.Sprintf("Falha ao gravar PID file: %v", err))
	}

	logMessage(fmt.Sprintf("Proxy iniciado na porta %d", port))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logMessage(fmt.Sprintf("Sinal recebido, encerrando proxy na porta %d", port))
		listener.Close()
		os.Remove(pidFile)
		os.Exit(0)
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			logMessage(fmt.Sprintf("Erro aceitando conexão na porta %d: %v", port, err))
			break
		}
		go tryProtocols(conn)
	}
	logMessage(fmt.Sprintf("Proxy encerrado na porta %d", port))
	os.Remove(pidFile)
}