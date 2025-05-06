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
	readTimeout = time.Second // Timeout para leitura inicial
)

var (
	logMutex  sync.Mutex
	sslConfig *tls.Config
	// canal para controlar encerramento do proxy
	stopChan chan struct{}
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

func isHTTPMethod(data string) bool {
	data = strings.ToUpper(data)
	methods := []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH", "HEAD", "TRACE", "CONNECT"}
	for _, m := range methods {
		if strings.HasPrefix(data, m+" ") {
			return true
		}
	}
	return false
}

// Função auxiliar para handshake TLS e obter conn decorada ou falha
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

// Tenta redirecionar a conexão usando diferentes protocolos com suporte TLS
func tryProtocols(conn net.Conn) {
	defer conn.Close()

	originalConn := conn

	resetConn := func(c net.Conn) net.Conn {
		conn1, conn2 := net.Pipe()
		go func() {
			io.Copy(conn1, c)
			conn1.Close()
		}()
		return conn2
	}

	// Protocolos que suportam TLS: WebSocket, MQTT, XMPP, HTTP/2, AMQP
	// Para cada protocolo: tentar TLS primeiro se sslConfig != nil, senão tentar sem TLS directly.

	// WebSocket
	if tryWebSocket(originalConn, true) {
		return
	}
	if tryWebSocket(originalConn, false) {
		return
	}

	originalConn = resetConn(originalConn)

	// MQTT
	if tryMQTT(originalConn, true) {
		return
	}
	if tryMQTT(originalConn, false) {
		return
	}

	originalConn = resetConn(originalConn)

	// XMPP
	if tryXMPP(originalConn, true) {
		return
	}
	if tryXMPP(originalConn, false) {
		return
	}

	originalConn = resetConn(originalConn)

	// HTTP/2
	if tryHTTP2(originalConn, true) {
		return
	}
	if tryHTTP2(originalConn, false) {
		return
	}

	originalConn = resetConn(originalConn)

	// AMQP
	if tryAMQP(originalConn, true) {
		return
	}
	if tryAMQP(originalConn, false) {
		return
	}

	originalConn = resetConn(originalConn)

	// SOCKS5 não tem suporte TLS tradicional, tenta apenas sem TLS
	if trySocks(originalConn) {
		return
	}

	originalConn = resetConn(originalConn)

	// TCP simples
	tryTCP(originalConn)
}

// Tenta redirecionar como WebSocket
func tryWebSocket(conn net.Conn, useTLS bool) bool {
	initialData, err := readInitialData(conn)
	if err != nil {
		logMessage(fmt.Sprintf("Erro leitura inicial WebSocket (TLS=%v): %v", useTLS, err))
		return false
	}

	if useTLS {
		tlsConn, err := tryTLSHandshake(&peekConn{Conn: conn, peeked: []byte(initialData)})
		if err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS WebSocket: %v", err))
			return false
		}
		conn = tlsConn
	}

	// Verificar Upgrade header de forma flexível e Connection header para permitir Keep-Alive
	lowerData := strings.ToLower(initialData)
	if strings.Contains(lowerData, "upgrade: websocket") {
		resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 101 WebSocket: " + err.Error())
			return false
		}
		logMessage(fmt.Sprintf("Conexão WebSocket estabelecida (TLS=%v)", useTLS))
		sshRedirect(conn)
		return true
	} else if strings.Contains(lowerData, "connection: upgrade") ||
		strings.Contains(lowerData, "connection: keep-alive") {
		resp := "HTTP/1.1 200 Connection Established\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 HTTP: " + err.Error())
			return false
		}
		logMessage(fmt.Sprintf("Conexão HTTP estabelecida (TLS=%v)", useTLS))
		sshRedirect(conn)
		return true
	}

	if isHTTPMethod(initialData) {
		resp := "HTTP/1.1 200 Connection Established\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 HTTP: " + err.Error())
			return false
		}
		logMessage(fmt.Sprintf("Conexão HTTP estabelecida (TLS=%v)", useTLS))
		sshRedirect(conn)
		return true
	}

	return false
}

// Tenta redirecionar como MQTT
func tryMQTT(conn net.Conn, useTLS bool) bool {
	initialBuf := make([]byte, 8192)
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	n, err := conn.Read(initialBuf)
	if err != nil {
		logMessage(fmt.Sprintf("Erro leitura inicial MQTT (TLS=%v): %v", useTLS, err))
		return false
	}
	conn.SetReadDeadline(time.Time{})

	initialData := initialBuf[:n]

	if useTLS {
		tlsConn, err := tryTLSHandshake(&peekConn{Conn: conn, peeked: initialData})
		if err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS MQTT: %v", err))
			return false
		}
		conn = tlsConn
	}

	if len(initialData) > 0 && initialData[0] == 0x10 { // MQTT Connect Packet
		resp := "HTTP/1.1 200 OK\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 MQTT: " + err.Error())
			return false
		}
		logMessage(fmt.Sprintf("Conexão MQTT estabelecida (TLS=%v)", useTLS))
		sshRedirect(conn)
		return true
	}

	return false
}

// Tenta redirecionar como XMPP
func tryXMPP(conn net.Conn, useTLS bool) bool {
	initialData, err := readInitialData(conn)
	if err != nil {
		logMessage(fmt.Sprintf("Erro leitura inicial XMPP (TLS=%v): %v", useTLS, err))
		return false
	}

	if useTLS {
		tlsConn, err := tryTLSHandshake(&peekConn{Conn: conn, peeked: []byte(initialData)})
		if err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS XMPP: %v", err))
			return false
		}
		conn = tlsConn
	}

	if strings.HasPrefix(initialData, "<stream:stream") {
		resp := "HTTP/1.1 200 OK\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 XMPP: " + err.Error())
			return false
		}
		logMessage(fmt.Sprintf("Conexão XMPP estabelecida (TLS=%v)", useTLS))
		sshRedirect(conn)
		return true
	}

	return false
}

// Tenta redirecionar como HTTP/2
func tryHTTP2(conn net.Conn, useTLS bool) bool {
	initialBuf := make([]byte, 8192)
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	n, err := conn.Read(initialBuf)
	if err != nil {
		logMessage(fmt.Sprintf("Erro leitura inicial HTTP/2 (TLS=%v): %v", useTLS, err))
		return false
	}
	conn.SetReadDeadline(time.Time{})

	initialData := initialBuf[:n]

	if useTLS {
		tlsConn, err := tryTLSHandshake(&peekConn{Conn: conn, peeked: initialData})
		if err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS HTTP/2: %v", err))
			return false
		}
		conn = tlsConn
	}

	if len(initialData) > 0 && initialData[0] == 0x50 { // HTTP/2 Magic Byte 'P'
		resp := "HTTP/1.1 200 OK\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 HTTP/2: " + err.Error())
			return false
		}
		logMessage(fmt.Sprintf("Conexão HTTP/2 estabelecida (TLS=%v)", useTLS))
		sshRedirect(conn)
		return true
	}

	return false
}

// Tenta redirecionar como AMQP
func tryAMQP(conn net.Conn, useTLS bool) bool {
	initialBuf := make([]byte, 8192)
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	n, err := conn.Read(initialBuf)
	if err != nil {
		logMessage(fmt.Sprintf("Erro leitura inicial AMQP (TLS=%v): %v", useTLS, err))
		return false
	}
	conn.SetReadDeadline(time.Time{})

	initialData := initialBuf[:n]

	if useTLS {
		tlsConn, err := tryTLSHandshake(&peekConn{Conn: conn, peeked: initialData})
		if err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS AMQP: %v", err))
			return false
		}
		conn = tlsConn
	}

	if len(initialData) > 0 && initialData[0] == 0x0A { // AMQP protocol header
		resp := "HTTP/1.1 200 OK\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 AMQP: " + err.Error())
			return false
		}
		logMessage(fmt.Sprintf("Conexão AMQP estabelecida (TLS=%v)", useTLS))
		sshRedirect(conn)
		return true
	}

	return false
}

// Tenta redirecionar como SOCKS5 (sem TLS)
func trySocks(conn net.Conn) bool {
	initialData, err := readInitialData(conn)
	if err != nil {
		logMessage(fmt.Sprintf("Erro leitura inicial SOCKS5: %v", err))
		return false
	}

	if len(initialData) > 0 && initialData[0] == 0x05 {
		resp := "HTTP/1.1 200 OK ProxyEuro\r\n\r\n"
		if _, err := conn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 SOCKS5: " + err.Error())
			return false
		}
		logMessage("Conexão SOCKS5 estabelecida")
		sshRedirect(conn)
		return true
	}

	return false
}

// TCP simples sem TLS
func tryTCP(conn net.Conn) {
	logMessage("Tentativa de conexão TCP simples")
	sshRedirect(conn)
}

// Redireciona a conexão para servidor SSH
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

// Systemd service path
func systemdServicePath(port int) string {
	return fmt.Sprintf("%s/proxyws@%d.service", serviceDir, port)
}

// Cria arquivo de service systemd
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

	// Criar canal para sinais de interrupção
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Goroutine para captura de sinais
	go func() {
		sig := <-sigCh
		logMessage(fmt.Sprintf("Sinal %v recebido, ignorando para manter proxy ativo", sig))
		// Não fecha o listener ou proxy para manter sempre escutando
		// Se desejar um desligamento controlado, implementar flag ou outro mecanismo
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			// Se erro temporário, continuar aceitando
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
