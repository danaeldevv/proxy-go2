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

// leitura inicial com timeout e captura dos dados para análise
func readInitialData(conn net.Conn) (string, error) {
	buf := make([]byte, 8192)
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	conn.SetReadDeadline(time.Time{}) // limpa deadline
	return string(buf[:n]), nil
}

// computa o valor do Sec-WebSocket-Accept a partir do Sec-WebSocket-Key
func computeAcceptKey(secWebSocketKey string) string {
	const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	h := sha1.New()
	h.Write([]byte(secWebSocketKey + magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// responde corretamente o handshake WebSocket com o cabeçalho Sec-WebSocket-Accept
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
	response := "HTTP/1.1 101 Switching Protocols\r\n"
	response += "Upgrade: websocket\r\n"
	response += "Connection: Upgrade\r\n"
	response += "Sec-WebSocket-Accept: " + acceptKey + "\r\n"
	response += "\r\n"

	_, err := conn.Write([]byte(response))
	return err
}

// Tenta redirecionar a conexão usando diferentes protocolos
func tryProtocols(conn net.Conn) {
	defer conn.Close()

	// Tenta WebSocket sem TLS
	if tryWebSocket(conn, false) {
		return
	}

	// Para as próximas tentativas, precisamos criar conexões independentes,
	// pois a anterior pode ter lido dados. Aqui simplificamos e encerramos se falhar.
	// Em um proxy real, isso precisaria ser melhor implementado.

	// Tenta WebSocket com TLS
	if tryWebSocket(conn, true) {
		return
	}

	// Tenta SOCKS5
	if trySocks(conn) {
		return
	}

	// Por fim, tenta TCP simples
	tryTCP(conn)
}

// Tenta redirecionar como WebSocket, diferenciando TLS ou não
func tryWebSocket(conn net.Conn, useTLS bool) bool {
	initialData, err := readInitialData(conn)
	if err != nil {
		logMessage(fmt.Sprintf("Erro na leitura inicial para WebSocket (TLS=%v): %v", useTLS, err))
		return false
	}

	if useTLS {
		if sslConfig == nil {
			logMessage("SSL Config não definida, não pode usar TLS para WebSocket")
			return false
		}
		tlsConn := tls.Server(&peekConn{Conn: conn, peeked: []byte(initialData)}, sslConfig)
		if err := tlsConn.Handshake(); err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS para WebSocket: %v", err))
			return false
		}
		conn = tlsConn
	} else {
		conn = &peekConn{Conn: conn, peeked: []byte(initialData)}
	}

	// Checa se a requisição HTTP é para WebSocket
	if strings.HasPrefix(initialData, "GET") || strings.HasPrefix(initialData, "CONNECT") {
		err := handleWebSocketHandshake(conn, initialData)
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

// Tenta redirecionar como SOCKS5
func trySocks(conn net.Conn) bool {
	initialData, err := readInitialData(conn)
	if err != nil {
		logMessage(fmt.Sprintf("Erro na leitura inicial para SOCKS5: %v", err))
		return false
	}

	if len(initialData) > 0 && initialData[0] == 0x05 {
		resp := "HTTP/1.1 200 OK\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 SOCKS5: " + err.Error())
			return false
		}
		logMessage("Conexão SOCKS5 estabelecida, redirecionando para SSH")
		sshRedirect(conn)
		return true
	}
	return false
}

// Tenta redirecionar como TCP simples (fallback)
func tryTCP(conn net.Conn) {
	logMessage("Tentativa de conexão TCP simples, redirecionando para SSH")
	resp := "HTTP/1.1 200 OK\r\n\r\n"
	_, _ = conn.Write([]byte(resp))
	sshRedirect(conn)
}

// redireciona para o OpenSSH na máquina local na porta 22
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

// Funções auxiliares para systemd e menu, mesmas anteriores
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