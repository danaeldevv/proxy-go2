package main

import (
	"bufio"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

const (
	sshHost       = "127.0.0.1:22"
	systemdDir    = "/etc/systemd/system"
	servicePrefix = "multiproxy-port-"
	certFile      = "server.crt"
	keyFile       = "server.key"
)

// showMenu prints the interactive menu with CPU/mem usage
func showMenu() {
	clearScreen()
	cpuUsage, memUsage := getSystemUsage()
	fmt.Printf("Multiproxy Manager - Uso CPU: %.2f%% | Uso Memória: %.2f MB\n", cpuUsage, memUsage)
	fmt.Println("1) Abrir Porta")
	fmt.Println("2) Fechar Porta")
	fmt.Println("3) Sair")
	fmt.Print("Escolha uma opção: ")
}

// clearScreen clears the terminal screen in a cross-platform way
func clearScreen() {
	print("\033[H\033[2J")
}

// getSystemUsage returns CPU usage % and memory usage MB for the current process
func getSystemUsage() (cpu float64, memMB float64) {
	// Approximate: Use runtime metrics as fallback (no external deps)
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)
	memMB = float64(memStats.Alloc) / (1024 * 1024)
	// For CPU usage, stdlib does not provide easy cross-platform way; return 0 as placeholder
	cpu = 0.0
	return
}

func main() {
	flagPort := flag.Int("port", 0, "Run proxy on this port (used by systemd services)")
	flagVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *flagVersion {
		fmt.Println("Multiproxy v1.0 - Go")
		return
	}

	if *flagPort > 0 {
		// Run proxy server on port (as systemd service)
		runProxy(*flagPort)
		return
	}

	// Otherwise run menu manager
	for {
		showMenu()
		var input string
		fmt.Scanln(&input)
		input = strings.TrimSpace(input)
		switch input {
		case "1":
			openPort()
		case "2":
			closePort()
		case "3":
			fmt.Println("Saindo...")
			return
		default:
			fmt.Println("Opção inválida, tente novamente.")
			time.Sleep(time.Second)
		}
	}
}

// openPort asks user for port and creates systemd service to run proxy there
func openPort() {
	clearScreen()
	fmt.Print("Digite a porta para abrir: ")
	var portStr string
	fmt.Scanln(&portStr)

	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port <= 0 || port > 65535 {
		fmt.Println("Porta inválida.")
		time.Sleep(2 * time.Second)
		return
	}

	// Check if systemdDir is writable
	if !isWritable(systemdDir) {
		fmt.Printf("Erro: sem permissão para escrever em %s. Execute como root.\n", systemdDir)
		time.Sleep(3 * time.Second)
		return
	}

	// Create systemd service content and write file
	servicePath := filepath.Join(systemdDir, fmt.Sprintf("%s%d.service", servicePrefix, port))
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Não foi possível obter caminho do executável.")
		return
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=Multiproxy service on port %d
After=network.target

[Service]
Type=simple
ExecStart=%s --port %d
Restart=always

[Install]
WantedBy=multi-user.target
`, port, exePath, port)

	err = os.WriteFile(servicePath, []byte(serviceContent), 0644)
	if err != nil {
		fmt.Println("Erro ao criar arquivo de serviço systemd:", err)
		return
	}

	// Reload systemd daemon
	exec.Command("systemctl", "daemon-reload").Run()

	// Enable and start service
	exec.Command("systemctl", "enable", fmt.Sprintf("%s%d.service", servicePrefix, port)).Run()
	exec.Command("systemctl", "start", fmt.Sprintf("%s%d.service", servicePrefix, port)).Run()

	fmt.Printf("Porta %d aberta com serviço systemd criado e iniciado.\n", port)
	fmt.Println("Aperte Enter para continuar...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

// closePort asks user for port and stops that systemd service and removes the unit file
func closePort() {
	clearScreen()
	fmt.Print("Digite a porta para fechar: ")
	var portStr string
	fmt.Scanln(&portStr)

	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port <= 0 || port > 65535 {
		fmt.Println("Porta inválida.")
		time.Sleep(2 * time.Second)
		return
	}

	serviceName := fmt.Sprintf("%s%d.service", servicePrefix, port)
	servicePath := filepath.Join(systemdDir, serviceName)

	// Stop and disable service
	exec.Command("systemctl", "stop", serviceName).Run()
	exec.Command("systemctl", "disable", serviceName).Run()

	// Remove service file
	err = os.Remove(servicePath)
	if err != nil {
		fmt.Println("Erro ao remover arquivo de serviço systemd:", err)
		return
	}

	// Reload systemd daemon
	exec.Command("systemctl", "daemon-reload").Run()

	fmt.Printf("Porta %d fechada e serviço systemd removido.\n", port)
	fmt.Println("Aperte Enter para continuar...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func isWritable(path string) bool {
	testFile := filepath.Join(path, ".writetest.tmp")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		return false
	}
	os.Remove(testFile)
	return true
}

// runProxy runs the proxy server on a given port handling WSS and SOCKS with TLS and forwarding to SSH
func runProxy(port int) {
	log.Printf("Iniciando proxy na porta %d\n", port)

	// Load TLS configuration
	tlsConfig, err := loadTLSConfig()
	if err != nil {
		log.Fatalf("Erro ao carregar cert TLS: %v", err)
	}

	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), tlsConfig)
	if err != nil {
		log.Fatalf("Erro ao abrir listener TLS: %v", err)
	}
	defer ln.Close()

	log.Printf("Proxy rodando (WSS + SOCKS) na porta %d\n", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Erro ao aceitar conexao: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

// loadTLSConfig loads cert and key from certFile and keyFile
func loadTLSConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}

// handleConnection determines protocol (WSS or SOCKS) and proxies accordingly
func handleConnection(conn net.Conn) {
	defer conn.Close()

	// Set deadline for initial protocol detection
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("Erro leitura inicial conexao: %v", err)
		return
	}
	conn.SetReadDeadline(time.Time{}) // remove deadline

	data := buf[:n]

	// Detect WebSocket handshake request: HTTP Upgrade request with "Connection: Upgrade" and "Upgrade: websocket"
	if isWebSocketRequest(data) {
		log.Printf("Conexao WebSocket detectada")
		handleWebSocket(conn, data)
		return
	}

	// Detect SOCKS proxy request by magic bytes (SOCKS5 starts with 0x05)
	if len(data) > 0 && data[0] == 0x05 {
		log.Printf("Conexao SOCKS detectada")
		handleSocks(conn, data)
		return
	}

	// Unknown protocol, close connection
	log.Printf("Protocolo desconhecido recebido, fechando conexao")
}

// isWebSocketRequest checks if data contains websocket upgrade HTTP headers
func isWebSocketRequest(data []byte) bool {
	dataStr := string(data)
	if !strings.HasPrefix(dataStr, "GET ") {
		return false
	}
	if strings.Contains(strings.ToLower(dataStr), "upgrade: websocket") &&
		strings.Contains(strings.ToLower(dataStr), "connection: upgrade") {
		return true
	}
	return false
}

// handleWebSocket respond with HTTP/1.1 101 Switching Protocols and proxy data to localhost:22 (OpenSSH)
func handleWebSocket(clientConn net.Conn, initialData []byte) {
	// Parse HTTP headers to get Sec-WebSocket-Key header
	headers := parseHTTPHeaders(string(initialData))
	key := headers["Sec-WebSocket-Key"]
	if key == "" {
		log.Printf("Sec-WebSocket-Key ausente, fechando conexao")
		return
	}

	// Send HTTP/1.1 101 Switching Protocols response
	acceptKey := computeAcceptKey(key)
	response := fmt.Sprintf(
		"HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: %s\r\n" +
			"\r\n", acceptKey)

	_, err := clientConn.Write([]byte(response))
	if err != nil {
		log.Printf("Erro enviar resposta WebSocket: %v", err)
		return
	}

	// Proxy WebSocket frames bidirectionally between client and OpenSSH
	proxyWebSocket(clientConn)
}

// parseHTTPHeaders parse HTTP headers from request string into map
func parseHTTPHeaders(req string) map[string]string {
	headers := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(req))
	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()
		if firstLine {
			firstLine = false
			continue
		}
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return headers
}

// computeAcceptKey computes Sec-WebSocket-Accept from Sec-WebSocket-Key as per RFC6455
func computeAcceptKey(key string) string {
	importedKey := strings.TrimSpace(key)
	magicGUID := "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	sha1Sum := sha1Sum([]byte(importedKey + magicGUID))
	return base64Encode(sha1Sum)
}

func sha1Sum(data []byte) []byte {
	h := sha1.New()
	h.Write(data)
	return h.Sum(nil)
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// proxyWebSocket opens connection to ssh on tcp and copy data with framing (WS frames)
func proxyWebSocket(clientConn net.Conn) {
	// Connect to ssh server
	sshConn, err := net.Dial("tcp", sshHost)
	if err != nil {
		log.Printf("Erro conectar ao OpenSSH: %v", err)
		clientConn.Close()
		return
	}
	defer sshConn.Close()

	wsConfig, err := websocket.NewConfig("wss://localhost/", "wss://localhost/")
	if err != nil {
		log.Printf("Erro criar config websocket: %v", err)
		return
	}

	wsConfig.Protocol = []string{}

	wsServer := websocket.Server{Handler: func(wsConn *websocket.Conn) {
		// Pipe data bidirectionally between wsConn and sshConn
		go copyData(wsConn, sshConn)
		copyData(sshConn, wsConn)
	}}

	wsServer.ServeHTTP(&singleConnListener{conn: clientConn}, nil)
}

// singleConnListener wraps net.Conn to make a net.Listener accepting single conn
type singleConnListener struct {
	conn net.Conn
	used bool
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	if l.used {
		return nil, fmt.Errorf("Listener already accepted")
	}
	l.used = true
	return l.conn, nil
}

func (l *singleConnListener) Close() error {
	return nil
}

func (l *singleConnListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

// copyData copies data from src to dst
func copyData(dst io.Writer, src io.Reader) {
	io.Copy(dst, src)
}

// handleSocks handles SOCKS5 connections, responds HTTP/1.1 200 OK and proxies to localhost:22
func handleSocks(clientConn net.Conn, initialData []byte) {
	// Send HTTP/1.1 200 OK response before socks handshake as requested
	_, err := clientConn.Write([]byte("HTTP/1.1 200 OK\r\n\r\n"))
	if err != nil {
		log.Printf("Erro enviar 200 OK para SOCKS: %v", err)
		return
	}

	var n int
	var buff [262]byte
	copy(buff[:], initialData)

	// Read methods count if initialData not enough
	if n = len(initialData); n < 2 {
		nn, err := clientConn.Read(buff[2:])
		if err != nil {
			log.Printf("Erro ler handshake SOCKS: %v", err)
			return
		}
		n += nn
	}

	// Check VER and NMETHODS (minimum 3 bytes total)
	if buff[0] != 0x05 {
		log.Printf("Protocolo nao suportado nao eh SOCKS5")
		return
	}

	// Send METHOD selection (no auth = 0x00)
	_, err = clientConn.Write([]byte{0x05, 0x00})
	if err != nil {
		log.Printf("Erro enviar metodo SOCKS: %v", err)
		return
	}

	// Read SOCKS request
	n, err = io.ReadAtLeast(clientConn, buff[:], 5)
	if err != nil {
		log.Printf("Erro ler request SOCKS: %v", err)
		return
	}

	if buff[1] != 0x01 {
		log.Printf("Comando SOCKS nao suportado: %x", buff[1])
		return
	}

	// Address parsing start
	atyp := buff[3]
	addr := ""
	offset := 4

	switch atyp {
	case 0x01: // IPv4
		if n < offset+4+2 {
			_, err = io.ReadFull(clientConn, buff[n:offset+4+2])
			if err != nil {
				log.Printf("Erro ler endereco IPv4 SOCKS: %v", err)
				return
			}
			n = offset + 6
		}
		addr = fmt.Sprintf("%d.%d.%d.%d", buff[offset], buff[offset+1], buff[offset+2], buff[offset+3])
		offset += 4
	case 0x03: // Domainname
		length := int(buff[offset])
		if n < offset+length+1+2 {
			extraLen := offset + length + 1 + 2 - n
			_, err = io.ReadFull(clientConn, buff[n:n+extraLen])
			if err != nil {
				log.Printf("Erro ler endereco dominio SOCKS: %v", err)
				return
			}
			n += extraLen
		}
		addr = string(buff[offset+1 : offset+1+length])
		offset += 1 + length
	case 0x04: // IPv6
		if n < offset+16+2 {
			_, err = io.ReadFull(clientConn, buff[n:offset+16+2])
			if err != nil {
				log.Printf("Erro ler endereco IPv6 SOCKS: %v", err)
				return
			}
			n = offset + 18
		}
		addr = net.IP(buff[offset : offset+16]).String()
		offset += 16
	default:
		log.Printf("Tipo endereco SOCKS desconhecido: %d", atyp)
		return
	}

	port := binary.BigEndian.Uint16(buff[offset : offset+2])
	_ = addr
	_ = port

	// respond success to client (bind to localhost 22)
	resp := []byte{
		0x05, 0x00, 0x00, 0x01,
		0, 0, 0, 0,
		0, 0,
	}
	copy(resp[4:8], net.ParseIP("0.0.0.0").To4())
	binary.BigEndian.PutUint16(resp[8:10], uint16(22))

	_, err = clientConn.Write(resp)
	if err != nil {
		log.Printf("Erro enviar resposta SOCKS: %v", err)
		return
	}

	// After handshake, connect to local SSH and proxy data
	sshConn, err := net.Dial("tcp", sshHost)
	if err != nil {
		log.Printf("Erro conectar OpenSSH: %v", err)
		return
	}
	defer sshConn.Close()
	proxyPair(clientConn, sshConn)
}

// proxyPair pipes data bidirectionally between two connections until error or close
func proxyPair(a, b net.Conn) {
	ch := make(chan struct{}, 2)
	go func() {
		io.Copy(a, b)
		ch <- struct{}{}
	}()
	go func() {
		io.Copy(b, a)
		ch <- struct{}{}
	}()
	<-ch
}

