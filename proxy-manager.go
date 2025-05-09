package main

import (
\t"bufio"
\t"bytes"
\t"crypto/tls"
\t"encoding/binary"
\t"flag"
\t"fmt"
\t"io"
\t"log"
\t"net"
\t"net/http"
\t"os"
\t"os/exec"
\t"path/filepath"
\t"runtime"
\t"strconv"
\t"strings"
\t"time"
\t"unicode"

\t"golang.org/x/net/websocket"
)

const (
\tsshHost = "127.0.0.1:22"
\tsystemdDir = "/etc/systemd/system"
\tservicePrefix = "multiproxy-port-"
\tcertFile = "server.crt"
\tkeyFile  = "server.key"
)

// showMenu prints the interactive menu with CPU/mem usage
func showMenu() {
\tclearScreen()
\tcpuUsage, memUsage := getSystemUsage()
\tfmt.Printf("Multiproxy Manager - CPU Usage: %.2f%% | Mem Usage: %.2f MB\n", cpuUsage, memUsage)
\tfmt.Println("1) Abrir Porta")
\tfmt.Println("2) Fechar Porta")
\tfmt.Println("3) Sair")
\tfmt.Print("Escolha uma opcao: ")
}

// clearScreen clears the terminal screen in a cross-platform way
func clearScreen() {
\tprint(\"\\033[H\\033[2J\")
}

// getSystemUsage returns CPU usage % and memory usage MB for the current process
func getSystemUsage() (cpu float64, memMB float64) {
\t// Approximate: Use runtime metrics as fallback (no external deps)
\tmemStats := &runtime.MemStats{}
\truntime.ReadMemStats(memStats)
\tmemMB = float64(memStats.Alloc) / (1024 * 1024)
\t// For CPU usage, we do not have easy cross-platform in stdlib. Just return 0 for now.
\tcpu = 0.0
\treturn
}

func main() {
\tflagPort := flag.Int(\"port\", 0, \"Run proxy on this port (used by systemd services)\")
\tflagVersion := flag.Bool(\"version\", false, \"Show version\")
\tflag.Parse()

\tif *flagVersion {
\t\tfmt.Println(\"Multiproxy v1.0 - Go\")
\t\treturn
\t}

\tif *flagPort > 0 {
\t\t// Run proxy server on port (as systemd service)
\t\trunProxy(*flagPort)
\t\treturn
\t}

\t// Otherwise run menu manager
\tfor {
\t\tshowMenu()
\t\tvar input string
\t\tfmt.Scanln(&input)
\t\tinput = strings.TrimSpace(input)
\t\tswitch input {
\t\tcase \"1\":
\t\t\topenPort()
\t\tcase \"2\":
\t\t\tclosePort()
\t\tcase \"3\":
\t\t\tfmt.Println(\"Saindo...\")
\t\t\treturn
\t\tdefault:
\t\t\tfmt.Println(\"Opcao invalida, tente novamente.\")
\t\t\ttime.Sleep(time.Second)
\t\t}
\t}
}

// openPort asks user for port and creates systemd service to run proxy there
func openPort() {
\tclearScreen()
\tfmt.Print(\"Digite a porta para abrir: \")
\tvar portStr string
\tfmt.Scanln(&portStr)

\tport, err := strconv.Atoi(strings.TrimSpace(portStr))
\tif err != nil || port <= 0 || port > 65535 {
\t\tfmt.Println(\"Porta invalida.\")
\t\ttime.Sleep(2 * time.Second)
\t\treturn
\t}

\t// Check if systemdDir is writable
\tif !isWritable(systemdDir) {
\t\tfmt.Printf(\"Erro: sem permissao para escrever em %s. Execute como root.\n\", systemdDir)
\t\ttime.Sleep(3 * time.Second)
\t\treturn
\t}

\t// Create systemd service content and write file
\tservicePath := filepath.Join(systemdDir, fmt.Sprintf(\"%s%d.service\", servicePrefix, port))
\texePath, err := os.Executable()
\tif err != nil {
\t\tfmt.Println(\"Nao foi possivel obter caminho do executavel.\")

\t\treturn
\t}

\tserviceContent := fmt.Sprintf(`[Unit]
Description=Multiproxy service on port %d
After=network.target

[Service]
Type=simple
ExecStart=%s --port %d
Restart=always

[Install]
WantedBy=multi-user.target
`, port, exePath, port)

\terr = os.WriteFile(servicePath, []byte(serviceContent), 0644)
\tif err != nil {
\t\tfmt.Println(\"Erro ao criar arquivo de serviço systemd:\", err)
\t\treturn
\t}

\t// Reload systemd daemon
\texec.Command(\"systemctl\", \"daemon-reload\").Run()

\t// Enable and start service
\texec.Command(\"systemctl\", \"enable\", fmt.Sprintf(\"%s%d.service\", servicePrefix, port)).Run()
\texec.Command(\"systemctl\", \"start\", fmt.Sprintf(\"%s%d.service\", servicePrefix, port)).Run()

\tfmt.Printf(\"Porta %d aberta e service systemd criado e iniciado.\n\", port)
\tfmt.Println(\"Aperte Enter para continuar...\")
\tbufio.NewReader(os.Stdin).ReadBytes('\\n')
}

// closePort asks user for port and stops that systemd service and removes the unit file
func closePort() {
\tclearScreen()
\tfmt.Print(\"Digite a porta para fechar: \")
\tvar portStr string
\tfmt.Scanln(&portStr)

\tport, err := strconv.Atoi(strings.TrimSpace(portStr))
\tif err != nil || port <= 0 || port > 65535 {
\t\tfmt.Println(\"Porta invalida.\")
\t\ttime.Sleep(2 * time.Second)
\t\treturn
\t}

\tserviceName := fmt.Sprintf(\"%s%d.service\", servicePrefix, port)
\tservicePath := filepath.Join(systemdDir, serviceName)

\t// Stop and disable service
\texec.Command(\"systemctl\", \"stop\", serviceName).Run()
\texec.Command(\"systemctl\", \"disable\", serviceName).Run()

\t// Remove service file
\terr = os.Remove(servicePath)
\tif err != nil {
\t\tfmt.Println(\"Erro ao remover arquivo de serviço systemd:\", err)
\t\treturn
\t}

\t// Reload systemd daemon
\texec.Command(\"systemctl\", \"daemon-reload\").Run()

\tfmt.Printf(\"Porta %d fechada e service systemd removido.\n\", port)
\tfmt.Println(\"Aperte Enter para continuar...\")
\tbufio.NewReader(os.Stdin).ReadBytes('\\n')
}

func isWritable(path string) bool {
\ttestFile := filepath.Join(path, \".writetest.tmp\")
\terr := os.WriteFile(testFile, []byte(\"test\"), 0644)
\tif err != nil {
\t\treturn false
\t}
\tos.Remove(testFile)
\treturn true
}

// runProxy runs the proxy server on a given port handling WSS and SOCKS with TLS and forwarding to SSH
func runProxy(port int) {
\tlog.Printf(\"Iniciando proxy na porta %d\\n\", port)

\t// Load TLS configuration
\ttlsConfig, err := loadTLSConfig()
\tif err != nil {
\t\tlog.Fatalf(\"Erro ao carregar cert TLS: %v\", err)
\t}

\tln, err := tls.Listen(\"tcp\", fmt.Sprintf(\":%d\", port), tlsConfig)
\tif err != nil {
\t\tlog.Fatalf(\"Erro ao abrir listener TLS: %v\", err)
\t}
\tdefer ln.Close()

\tlog.Printf(\"Proxy rodando (WSS + SOCKS) na porta %d\\n\", port)
\tfor {
\t\tconn, err := ln.Accept()
\t\tif err != nil {
\t\t\tlog.Printf(\"Erro ao aceitar conexao: %v\", err)
\t\t\tcontinue
\t\t}
\t\tgo handleConnection(conn)
\t}
}

// loadTLSConfig loads cert and key from certFile and keyFile
func loadTLSConfig() (*tls.Config, error) {
\tcert, err := tls.LoadX509KeyPair(certFile, keyFile)
\tif err != nil {
\t\treturn nil, err
\t}
\treturn &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}

// handleConnection determines protocol (WSS or SOCKS) and proxies accordingly
func handleConnection(conn net.Conn) {
\tdefer conn.Close()

\t// Set deadline for initial protocol detection
\tconn.SetReadDeadline(time.Now().Add(5 * time.Second))
\tbuf := make([]byte, 1024)
\tn, err := conn.Read(buf)
\tif err != nil {
\t\tlog.Printf(\"Erro leitura inicial conexao: %v\", err)
\t\treturn
\t}
\tconn.SetReadDeadline(time.Time{}) // remove deadline

\tdata := buf[:n]

\t// Detect WebSocket handshake request: HTTP Upgrade request with "Connection: Upgrade" and "Upgrade: websocket"
\tif isWebSocketRequest(data) {
\t\tlog.Printf(\"Conexao WebSocket detectada\")
\t\thandleWebSocket(conn, data)
\t\treturn
\t}

\t// Detect SOCKS proxy request by magic bytes (SOCKS5 starts with 0x05)
\tif len(data) > 0 && data[0] == 0x05 {
\t\tlog.Printf(\"Conexao SOCKS detectada\")
\t\thandleSocks(conn, data)
\t\treturn
\t}

\t// Unknown protocol, close connection
\tlog.Printf(\"Protocolo desconhecido recebido, fechando conexao\")
}

// isWebSocketRequest checks if data contains websocket upgrade HTTP headers
func isWebSocketRequest(data []byte) bool {
\tdataStr := string(data)
\tif !strings.HasPrefix(dataStr, \"GET \") {
\t\treturn false
\t}
\tif strings.Contains(strings.ToLower(dataStr), \"upgrade: websocket\") &&
\t\tstrings.Contains(strings.ToLower(dataStr), \"connection: upgrade\") {
\t\treturn true
\t}
\treturn false
}

// handleWebSocket respond with HTTP/1.1 101 Switching Protocols and proxy data to localhost:22 (OpenSSH)
func handleWebSocket(clientConn net.Conn, initialData []byte) {
\t// Parse HTTP headers to get Sec-WebSocket-Key header
\theaders := parseHTTPHeaders(string(initialData))
\tkey := headers[\"Sec-WebSocket-Key\"]
\tif key == \"\" {
\t\tlog.Printf(\"Sec-WebSocket-Key ausente, fechando conexao\")
\t\treturn
\t}

\t// Send HTTP/1.1 101 Switching Protocols response
\tacceptKey := computeAcceptKey(key)
\tresponse := fmt.Sprintf(
\t\t\"HTTP/1.1 101 Switching Protocols\\r\\n\" +
\t\t\"Upgrade: websocket\\r\\n\" +
\t\t\"Connection: Upgrade\\r\\n\" +
\t\t\"Sec-WebSocket-Accept: %s\\r\\n\" +
\t\t\"\\r\\n\", acceptKey)

\t_, err := clientConn.Write([]byte(response))
\tif err != nil {
\t\tlog.Printf(\"Erro enviar resposta WebSocket: %v\", err)
\t\treturn
\t}

\t// Proxy WebSocket frames bidirectionally between client and OpenSSH
\tproxyWebSocket(clientConn)
}

// parseHTTPHeaders parse HTTP headers from request string into map
func parseHTTPHeaders(req string) map[string]string {
\theaders := make(map[string]string)
\tscanner := bufio.NewScanner(strings.NewReader(req))
\tfirstLine := true
\tfor scanner.Scan() {
\t\tline := scanner.Text()
\t\tif firstLine {
\t\t\tfirstLine = false
\t\t\tcontinue
\t\t}
\t\tif line == \"\" {
\t\t\tbreak
\t\t}
\t\tparts := strings.SplitN(line, \":\", 2)
\t\tif len(parts) == 2 {
\t\t\theaders[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
\t\t}
\t}
\treturn headers
}

// computeAcceptKey computes Sec-WebSocket-Accept from Sec-WebSocket-Key as per RFC6455
func computeAcceptKey(key string) string {
\timportedKey := strings.TrimSpace(key)
\tmagicGUID := \"258EAFA5-E914-47DA-95CA-C5AB0DC85B11\"
\tsha1Sum := sha1Sum([]byte(importedKey + magicGUID))
\treturn base64Encode(sha1Sum)
}

func sha1Sum(data []byte) []byte {
\timport \"crypto/sha1\"
\th := sha1.New()
\th.Write(data)
\treturn h.Sum(nil)
}

func base64Encode(data []byte) string {
\timport \"encoding/base64\"
\treturn base64.StdEncoding.EncodeToString(data)
}

// proxyWebSocket opens connection to ssh on tcp and copy data with framing (WS frames)
func proxyWebSocket(clientConn net.Conn) {
\t// Connect to ssh server
\tsshConn, err := net.Dial(\"tcp\", sshHost)
\tif err != nil {
\t\tlog.Printf(\"Erro conectar ao OpenSSH: %v\", err)
\t\tclientConn.Close()
\t\treturn
\t}
\tdefer sshConn.Close()

\t// Initialize websocket connection over net.Conn for client (insecure, already TLS outside)
\twsConfig, err := websocket.NewConfig(\"wss://localhost/\", \"wss://localhost/\")
\tif err != nil {
\t\tlog.Printf(\"Erro criar config websocket: %v\", err)
\t\treturn
\t}

\twsConfig.Protocol = []string{}

\twsWS := websocket.Server{Handler: func(wsConn *websocket.Conn) {
\t\t// Pipe data bidirectionally between wsConn and sshConn
\t\tgo copyData(wsConn, sshConn)
\t\tcopyData(sshConn, wsConn)
\t}}

\twsWS.ServeHTTP(&singleConnListener{conn: clientConn}, nil)
}

// singleConnListener wraps net.Conn to make a net.Listener accepting single conn
type singleConnListener struct {
\tconn net.Conn
\tused bool
}

func (l *singleConnListener) Accept() (net.Conn, error) {
\tif l.used {
\t\treturn nil, fmt.Errorf(\"Listener already accepted\")
\t}
\tl.used = true
\treturn l.conn, nil
}

func (l *singleConnListener) Close() error {
\treturn nil
}

func (l *singleConnListener) Addr() net.Addr {
\treturn l.conn.LocalAddr()
}

// copyData copies data from src to dst
func copyData(dst io.Writer, src io.Reader) {
\tio.Copy(dst, src)
}

// handleSocks handles SOCKS5 connections, responds HTTP/1.1 200 OK and proxies to localhost:22
func handleSocks(clientConn net.Conn, initialData []byte) {
\t// SOCKS5 handshake with client

\t// Send HTTP/1.1 200 OK response before socks handshake per user request
\t_, err := clientConn.Write([]byte(\"HTTP/1.1 200 OK\\r\\n\\r\\n\"))
\tif err != nil {
\t\tlog.Printf(\"Erro enviar 200 OK para SOCKS: %v\", err)
\t\treturn
\t}

\t// Now process SOCKS5 handshake with client as usual
\tvar n int
\tvar buff [262]byte
\tcopy(buff[:], initialData)

\t// Read methods count
\tif n = len(initialData); n < 2 {
\t\tn, err = clientConn.Read(buff[2:])
\t\tif err != nil {
\t\t\tlog.Printf(\"Erro ler handshake SOCKS: %v\", err)
\t\t\treturn
\t\t}
\t\tn += n
\t}

\t// Check VER and NMETHODS (minimum 3 bytes total)
\tif buff[0] != 0x05 {
\t\tlog.Printf(\"Protocolo nao suportado nao eh SOCKS5\")
\t\treturn
\t}

\t// Send METHOD selection (no auth = 0x00)
\t_, err = clientConn.Write([]byte{0x05, 0x00})
\tif err != nil {
\t\tlog.Printf(\"Erro enviar metodo SOCKS: %v\", err)
\t\treturn
\t}

\t// Read SOCKS request
\tn, err = io.ReadAtLeast(clientConn, buff[:], 5)
\tif err != nil {
\t\tlog.Printf(\"Erro ler request SOCKS: %v\", err)
\t\treturn
\t}

\tif buff[1] != 0x01 {
\t\tlog.Printf(\"Comando SOCKS nao suportado: %x\", buff[1])
\t\treturn
\t}

\t// Address parsing start
\tatyp := buff[3]
\taddr := \"\"
\toffset := 4

\tswitch atyp {
\tcase 0x01: // IPv4
\t\tif n < offset+4+2 {
\t\t\t_, err = io.ReadFull(clientConn, buff[n:offset+4+2])
\t\t\tif err != nil {
\t\t\t\tlog.Printf(\"Erro ler endereco IPv4 SOCKS: %v\", err)
\t\t\t\treturn
\t\t\t}
\t\t\tn = offset + 6
\t\t}
\t\taddr = fmt.Sprintf(\"%d.%d.%d.%d\", buff[offset], buff[offset+1], buff[offset+2], buff[offset+3])
\t\toffset += 4
\tcase 0x03: // Domainname
\t\tlength := int(buff[offset])
\t\tif n < offset+length+1+2 {
\t\t\textraLen := offset + length + 1 + 2 - n
\t\t\t_, err = io.ReadFull(clientConn, buff[n:n+extraLen])
\t\t\tif err != nil {
\t\t\t\tlog.Printf(\"Erro ler endereco dominio SOCKS: %v\", err)
\t\t\t\treturn
\t\t\t}
\t\t\tn += extraLen
\t\t}
\t\taddr = string(buff[offset+1 : offset+1+length])
\t\toffset += 1 + length
\tcase 0x04: // IPv6
\t\tif n < offset+16+2 {
\t\t\t_, err = io.ReadFull(clientConn, buff[n:offset+16+2])
\t\t\tif err != nil {
\t\t\t\tlog.Printf(\"Erro ler endereco IPv6 SOCKS: %v\", err)
\t\t\t\treturn
\t\t\t}
\t\t\tn = offset + 18
\t\t}
\t\taddr = net.IP(buff[offset : offset+16]).String()
\t\toffset += 16
\tdefault:
\t\tlog.Printf(\"Tipo endereco SOCKS desconhecido: %d\", atyp)
\t\treturn
\t}

\tport := binary.BigEndian.Uint16(buff[offset : offset+2])
\t// respond success to client (bind to localhost 22)
\tresp := []byte{
\t\t0x05, 0x00, 0x00, 0x01,
\t\t0, 0, 0, 0,
\t\t0, 0,
\t}
\tcopy(resp[4:8], net.ParseIP(\"0.0.0.0\").To4())
\tbinary.BigEndian.PutUint16(resp[8:10], uint16(22))

\t_, err = clientConn.Write(resp)
\tif err != nil {
\t\tlog.Printf(\"Erro enviar resposta SOCKS: %v\", err)
\t\treturn
\t}

\t// After handshake, connect to local SSH and proxy data
\tsshConn, err := net.Dial(\"tcp\", sshHost)
\tif err != nil {
\t\tlog.Printf(\"Erro conectar OpenSSH: %v\", err)
\t\treturn
\t}
\tdefer sshConn.Close()
\tproxyPair(clientConn, sshConn)
}

// proxyPair pipes data bidirectionally between two connections until error or close
func proxyPair(a, b net.Conn) {
\tch := make(chan struct{}, 2)
\tgo func() {
\t\tio.Copy(a, b)
\t\tch <- struct{}{}
\t}()
\tgo func() {
\t\tio.Copy(b, a)
\t\tch <- struct{}{}
\t}()
\t<-ch
}


