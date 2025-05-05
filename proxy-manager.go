package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	mutex    sync.Mutex
	servicos = make(map[int]bool)
	clearCmd = exec.Command("clear")
)

func generateCerts(port int) (*tls.Config, error) {
	portStr := strconv.Itoa(port)
	certDir := filepath.Join("/etc/proxyeuro", portStr)
	certFile := filepath.Join(certDir, "cert.pem")
	keyFile := filepath.Join(certDir, "key.pem")

	// Remover certificados existentes
	if _, err := os.Stat(certDir); !os.IsNotExist(err) {
		os.RemoveAll(certDir)
	}
	os.MkdirAll(certDir, 0755)

	// Gerar chave privada
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("erro ao gerar chave privada: %v", err)
	}

	// Criar template do certificado
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"ProxyEuro"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames: []string{"localhost"},
	}

	// Gerar certificado
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar certificado: %v", err)
	}

	// Salvar certificado
	certOut, err := os.Create(certFile)
	if err != nil {
		return nil, fmt.Errorf("erro ao salvar certificado: %v", err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	// Salvar chave privada
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("erro ao salvar chave privada: %v", err)
	}
	pem.Encode(keyOut, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	keyOut.Close()

	// Carregar certificado
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("erro ao carregar certificado: %v", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
	}, nil
}

func clearScreen() {
	clearCmd.Stdout = os.Stdout
	clearCmd.Run()
}

func centerText(text string, width int) string {
	padding := (width - len(text)) / 2
	if padding < 0 {
		padding = 0
	}
	return strings.Repeat(" ", padding) + text
}

func printMenu() {
	clearScreen()
	width := 50
	fmt.Println(centerText("==== MENU PROXY EURO ====", width))
	fmt.Println(centerText("1. Abrir porta", width))
	fmt.Println(centerText("2. Fechar porta", width))
	fmt.Println(centerText("3. Monitorar portas abertas", width))
	fmt.Println(centerText("4. Sair", width))
	fmt.Print("\nEscolha: ")
}

func handleConnection(conn net.Conn, tlsConfig *tls.Config) {
	buffer := make([]byte, 1024)
	_, err := conn.Read(buffer)
	if err != nil {
		log.Printf("Erro ao ler dados: %v", err)
		conn.Close()
		return
	}

	data := buffer[:]

	if isTLS(data) {
		tlsConn := tls.Server(conn, tlsConfig)
		err := tlsConn.Handshake()
		if err != nil {
			log.Printf("Handshake TLS falhou: %v", err)
			conn.Close()
			return
		}
		log.Println("🔒 Conexão TLS detectada")
		handleProtocol(tlsConn, data)
	} else {
		log.Println("🔓 Conexão sem TLS detectada")
		handleProtocol(conn, data)
	}
}

func handleProtocol(conn net.Conn, data []byte) {
	switch {
	case isWebSocket(data):
		log.Println("🌐 Conexão WebSocket detectada")
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))
	case isSOCKS5(data):
		log.Println("🧦 Conexão SOCKS5 detectada")
		handleSOCKS5(conn, data)
	case isHTTP101(data) || isHTTP200(data):
		log.Println("📄 Conexão HTTP detectada")
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nProxyEuro ativo\n"))
	default:
		log.Println("❌ Protocolo desconhecido")
		conn.Close()
	}
}

func handleSOCKS5(conn net.Conn, data []byte) {
	conn.Write([]byte{0x05, 0x00})
	buffer := make([]byte, 1024)
	_, err := conn.Read(buffer)
	if err != nil {
		log.Printf("Erro SOCKS5: %v", err)
		conn.Close()
		return
	}
	conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, 0x1F, 0x90})
}

func isTLS(data []byte) bool {
	return len(data) > 0 && data[0] == 0x16
}

func isHTTP101(data []byte) bool {
	return strings.HasPrefix(string(data), "HTTP/1.1 101")
}

func isHTTP200(data []byte) bool {
	return strings.HasPrefix(string(data), "HTTP/1.1 200")
}

func isWebSocket(data []byte) bool {
	return strings.HasPrefix(string(data), "GET / HTTP/1.1")
}

func isSOCKS5(data []byte) bool {
	return len(data) > 0 && data[0] == 0x05
}

func monitorarPortas() {
	mutex.Lock()
	defer mutex.Unlock()

	if len(servicos) == 0 {
		fmt.Println("Nenhuma porta está ativa.")
		return
	}

	fmt.Println("\nPortas ativas:")
	for port := range servicos {
		fmt.Printf("- Porta %d (serviço: proxyeuro@%d.service)\n", port, port)
	}
}

func abrirPorta(port int) {
	mutex.Lock()
	defer mutex.Unlock()

	if servicos[port] {
		fmt.Println("Porta já está em uso.")
		return
	}

	serviceName := fmt.Sprintf("proxyeuro@%d.service", port)
	servicePath := fmt.Sprintf("/etc/systemd/system/%s", serviceName)

	serviceContent := fmt.Sprintf(`[Unit]
Description=ProxyEuro na porta %d
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/proxyeuro %d
Restart=always

[Install]
WantedBy=multi-user.target
`, port, port)

	err := os.WriteFile(servicePath, []byte(serviceContent), 0644)
	if err != nil {
		fmt.Println("Erro ao criar serviço:", err)
		return
	}

	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", serviceName).Run()
	err = exec.Command("systemctl", "start", serviceName).Run()
	if err != nil {
		fmt.Println("Erro ao iniciar serviço:", err)
		return
	}

	servicos[port] = true
	fmt.Printf("Porta %d aberta com sucesso!\n", port)
}

func fecharPorta(port int) {
	mutex.Lock()
	defer mutex.Unlock()

	if !servicos[port] {
		fmt.Println("Porta não está ativa.")
		return
	}

	serviceName := fmt.Sprintf("proxyeuro@%d.service", port)
	exec.Command("systemctl", "stop", serviceName).Run()
	exec.Command("systemctl", "disable", serviceName).Run()
	os.Remove(fmt.Sprintf("/etc/systemd/system/%s", serviceName))
	exec.Command("systemctl", "daemon-reload").Run()
	delete(servicos, port)
	fmt.Printf("Porta %d fechada com sucesso!\n", port)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Uso: proxyeuro <porta>")
		os.Exit(1)
	}

	port, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Fatalf("Porta inválida: %v", err)
	}

	tlsConfig, err := generateCerts(port)
	if err != nil {
		log.Fatalf("Erro nos certificados: %v", err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("Erro ao iniciar servidor: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Erro na conexão: %v", err)
				continue
			}
			go handleConnection(conn, tlsConfig)
		}
	}()

	reader := bufio.NewReader(os.Stdin)
	for {
		printMenu()
		input, _ := reader.ReadString('\n')
		option := strings.TrimSpace(input)

		switch option {
		case "1":
			fmt.Print("Digite a porta para abrir: ")
			portStr, _ := reader.ReadString('\n')
			newPort, _ := strconv.Atoi(strings.TrimSpace(portStr))
			abrirPorta(newPort)
		case "2":
			fmt.Print("Digite a porta para fechar: ")
			portStr, _ := reader.ReadString('\n')
			closePort, _ := strconv.Atoi(strings.TrimSpace(portStr))
			fecharPorta(closePort)
		case "3":
			monitorarPortas()
			fmt.Print("\nPressione Enter para continuar...")
			reader.ReadString('\n')
		case "4":
			fmt.Println("Encerrando...")
			return
		default:
			fmt.Println("Opção inválida!")
		}
	}
}
