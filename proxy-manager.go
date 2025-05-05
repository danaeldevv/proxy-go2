package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
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
	mutex     sync.Mutex
	servicos  = make(map[int]bool)
	clearCmd  = exec.Command("clear")
	runAsService bool
)

func generateCerts(port int) (*tls.Config, error) {
	certDir := filepath.Join("/etc/proxyeuro", strconv.Itoa(port))
	certFile := filepath.Join(certDir, "cert.pem")
	keyFile := filepath.Join(certDir, "key.pem")

	os.RemoveAll(certDir)
	os.MkdirAll(certDir, 0755)

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("erro ao gerar chave: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{Organization: []string{"ProxyEuro"}},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:    []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar certificado: %v", err)
	}

	certOut, err := os.Create(certFile)
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	pem.Encode(keyOut, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	keyOut.Close()

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("erro ao carregar certificado: %v", err)
	}

	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}

func clearScreen() {
	clearCmd.Stdout = os.Stdout
	clearCmd.Run()
}

func printMenu() {
	clearScreen()
	fmt.Println(`
=== MENU PROXY EURO ===
1. Abrir porta
2. Fechar porta
3. Monitorar portas
4. Sair
`)
	fmt.Print("Escolha: ")
}

func handleConnection(conn net.Conn, tlsConfig *tls.Config) {
	defer conn.Close()
	buffer := make([]byte, 1024)
	
	if _, err := conn.Read(buffer); err != nil {
		log.Printf("Erro de leitura: %v", err)
		return
	}

	if buffer[0] == 0x16 {
		tlsConn := tls.Server(conn, tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			log.Printf("Erro TLS: %v", err)
			return
		}
		handleProtocol(tlsConn)
	} else {
		handleProtocol(conn)
	}
}

func handleProtocol(conn net.Conn) {
	switch {
	case strings.Contains(conn.RemoteAddr().String(), "http"):
		conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\nProxyEuro Ativo\n"))
	case strings.Contains(conn.RemoteAddr().String(), "websocket"):
		conn.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"))
	default:
		conn.Write([]byte("Protocolo não suportado\n"))
	}
}

func runService(port int) {
	tlsConfig, err := generateCerts(port)
	if err != nil {
		log.Fatalf("Erro certificado: %v", err)
	}

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		log.Fatalf("Erro ao ouvir porta: %v", err)
	}
	defer listener.Close()

	log.Printf("Serviço iniciado na porta %d", port)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Erro de conexão: %v", err)
			continue
		}
		go handleConnection(conn, tlsConfig)
	}
}

func manageService(action string, port int) {
	service := fmt.Sprintf("proxyeuro@%d.service", port)
	cmd := exec.Command("systemctl", action, service)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Erro ao %s serviço: %s\n%s", action, err, output)
	}
}

func monitorServices() {
	cmd := exec.Command("systemctl", "list-units", "--type=service", "--all", "proxyeuro@*")
	output, _ := cmd.CombinedOutput()
	
	fmt.Println("\nPortas ativas:")
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "proxyeuro@") {
			parts := strings.Fields(line)
			port := strings.TrimSuffix(strings.Split(parts[0], "@")[1], ".service")
			fmt.Printf("- Porta %s (%s)\n", port, parts[3])
		}
	}
}

func main() {
	flag.BoolVar(&runAsService, "service", false, "Modo serviço systemd")
	flag.Parse()

	if runAsService {
		if len(os.Args) < 3 {
			log.Fatal("Uso: proxyeuro --service <porta>")
		}
		port, _ := strconv.Atoi(os.Args[2])
		runService(port)
		return
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		printMenu()
		option, _ := reader.ReadString('\n')
		option = strings.TrimSpace(option)

		switch option {
		case "1":
			fmt.Print("Porta para abrir: ")
			portStr, _ := reader.ReadString('\n')
			port, _ := strconv.Atoi(strings.TrimSpace(portStr))
			manageService("start", port)
			manageService("enable", port)
			fmt.Println("Porta aberta com sucesso!")

		case "2":
			fmt.Print("Porta para fechar: ")
			portStr, _ := reader.ReadString('\n')
			port, _ := strconv.Atoi(strings.TrimSpace(portStr))
			manageService("stop", port)
			manageService("disable", port)
			fmt.Println("Porta fechada com sucesso!")

		case "3":
			monitorServices()
			fmt.Print("\nPressione Enter para continuar...")
			reader.ReadString('\n')

		case "4":
			os.Exit(0)

		default:
			fmt.Println("Opção inválida!")
		}
	}
}
