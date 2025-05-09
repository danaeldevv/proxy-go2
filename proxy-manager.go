package main

import (
	"bufio"
	"crypto/tls"
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
	readTimeout = 5 * time.Second
)

var (
	logMutex  sync.Mutex
	sslConfig *tls.Config
	stopChan  chan struct{}
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

func tryTLSHandshake(conn net.Conn) (net.Conn, error) {
	if sslConfig == nil {
		return nil, fmt.Errorf("sslConfig não definido")
	}
	tlsConn := tls.Server(conn, sslConfig)
	if err := tlsConn.Handshake(); err != nil {
		return nil, err
	}
	return tlsConn, nil
}

func tryProtocols(conn net.Conn) {
	defer conn.Close()

	if tryWebSocket(conn, true) {
		return
	}

	if trySocks(conn, true) {
		return
	}

	if trySocks(conn, false) {
		return
	}

	if tryHTTP(conn) {
		return
	}

	tryTCP(conn)
}

func tryWebSocket(conn net.Conn, useTLS bool) bool {
	var initialData string
	var err error

	if useTLS {
		tlsConn, err := tryTLSHandshake(conn)
		if err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS WebSocket: %v", err))
			return false
		}
		initialData, err = readInitialData(tlsConn)
		if err != nil {
			logMessage(fmt.Sprintf("Erro leitura inicial WebSocket pós-handshake TLS: %v", err))
			return false
		}
		conn = tlsConn
	} else {
		initialData, err = readInitialData(conn)
		if err != nil {
			logMessage(fmt.Sprintf("Erro leitura inicial WebSocket: %v", err))
			return false
		}
	}

	headers := parseHeaders(initialData)

	upg, upgOk := headers["upgrade"]
	connHdr, connOk := headers["connection"]

	if upgOk && upg == "websocket" && connOk && strings.Contains(connHdr, "upgrade") {
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
		_, err = conn.Write([]byte(resp))
		if err != nil {
			logMessage("Erro enviando resposta 101 Switching Protocols: " + err.Error())
			return false
		}
		logMessage("Conexão WebSocket estabelecida com resposta HTTP/1.1 101 Switching Protocols")
		sshRedirect(conn)
		return true
	}

	return false
}

func trySocks(conn net.Conn, useTLS bool) bool {
	var err error
	var initialData string

	if useTLS {
		tlsConn, err := tryTLSHandshake(conn)
		if err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS SOCKS5: %v", err))
			return false
		}
		initialData, err = readInitialData(tlsConn)
		if err != nil {
			logMessage(fmt.Sprintf("Erro leitura inicial SOCKS5 pós-handshake TLS: %v", err))
			return false
		}
		conn = tlsConn
	} else {
		initialData, err = readInitialData(conn)
		if err != nil {
			logMessage(fmt.Sprintf("Erro leitura inicial SOCKS5: %v", err))
			return false
		}
	}

	if len(initialData) > 0 && initialData[0] == 0x05 {
		resp := "HTTP/1.1 200 Connection Established\r\n\r\n"
		_, err = conn.Write([]byte(resp))
		if err != nil {
			logMessage("Erro enviando resposta 200 Connection Established: " + err.Error())
			return false
		}
		logMessage(fmt.Sprintf("Conexão SOCKS5 estabelecida (TLS=%v)", useTLS))
		sshRedirect(conn)
		return true
	}
	return false
}

func tryHTTP(conn net.Conn) bool {
	initialData, err := readInitialData(conn)
	if err != nil {
		logMessage(fmt.Sprintf("Erro leitura inicial HTTP: %v", err))
		return false
	}

	// Verifica se inicia com métodos HTTP comuns
	methods := []string{"GET ", "POST ", "HEAD ", "OPTIONS ", "PUT ", "DELETE ", "CONNECT "}
	validHTTP := false
	for _, m := range methods {
		if strings.HasPrefix(strings.ToUpper(initialData), m) {
			validHTTP = true
			break
		}
	}
	if !validHTTP {
		return false
	}

	resp := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\nProxy is working!"
	_, err = conn.Write([]byte(resp))
	if err != nil {
		logMessage("Erro enviando resposta HTTP: " + err.Error())
		return false
	}
	logMessage("Conexão HTTP estabelecida com resposta 200 OK")
	return true
}

func tryTCP(conn net.Conn) {
	logMessage("Tentativa de conexão TCP simples")
	sshRedirect(conn)
}

func sshRedirect(conn net.Conn) {
	serverConn, err := net.Dial("tcp", "127.0.0.1:22")
	if err != nil {
		logMessage(fmt.Sprintf("Erro conectando servidor SSH: %v", err))
		return
	}
	defer serverConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(serverConn, conn)
	}()

	go func() {
		defer wg.Done()
		io.Copy(conn, serverConn)
	}()

	wg.Wait()
	logMessage("Conexão redirecionada para o servidor SSH finalizada")
}

func parseHeaders(data string) map[string]string {
	headers := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		colon := strings.Index(line, ":")
		if colon > 0 {
			key := strings.ToLower(strings.TrimSpace(line[:colon]))
			value := strings.ToLower(strings.TrimSpace(line[colon+1:]))
			headers[key] = value
		}
	}
	return headers
}

func systemdServicePath(port int) string {
	return fmt.Sprintf("%s/proxyws@%d.service", serviceDir, port)
}

func createSystemdService(port int, execPath string) error {
	serviceContent := fmt.Sprintf(`[Unit]
Description=ProxyWS na porta %d (WebSocket Security + SOCKS5 Secure)
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
	_ = cmd.Run()
	cmd = exec.Command("systemctl", "disable", serviceName)
	_ = cmd.Run()
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
		stopChan = make(chan struct{})
		startProxy(port)
		return
	}

	execPath, _ := os.Executable()
	scanner := bufio.NewScanner(os.Stdin)

	for {
		clearScreen()
		fmt.Println("============================")
		fmt.Println("      Proxy Euro Verção 1.0")
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
			fmt.Printf("✅ Proxy iniciado na porta %d (WebSocket Security + SOCKS5 Secure)\n", port)
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

	logMessage(fmt.Sprintf("Proxy iniciado na porta %d (WebSocket Security + SOCKS5 Secure)", port))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			sig := <-sigCh
			logMessage(fmt.Sprintf("Sinal %v recebido, ignorando para manter proxy ativo", sig))
		}
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				logMessage(fmt.Sprintf("Erro temporário aceitando conexão na porta %d: %v", port, err))
				time.Sleep(50 * time.Millisecond)
				continue
			}
			logMessage(fmt.Sprintf("Erro fatal aceitando conexão na porta %d: %v", port, err))
			break
		}
		go tryProtocols(conn)
	}
	logMessage(fmt.Sprintf("Proxy encerrado na porta %d", port))
	os.Remove(pidFile)
}

