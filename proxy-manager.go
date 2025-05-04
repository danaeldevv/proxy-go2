package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"bufio"
	"sync"
)

var (
	mutex     sync.Mutex
	servicos  = make(map[int]bool)
	clearCmd  = exec.Command("clear")
)

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
	n, err := conn.Read(buffer)
	if err != nil {
		log.Printf("Erro ao ler dados iniciais: %v", err)
		conn.Close()
		return
	}

	data := buffer[:n]

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
	n, err := conn.Read(buffer)
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

	cmd := exec.Command("systemctl", "daemon-reexec")
	cmd.Run()

	serviceContent := fmt.Sprintf(`[Unit]
Description=ProxyEuro na porta %d
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/proxy_worker %d
Restart=always

[Install]
WantedBy=multi-user.target
`, port, port)

	servicePath := fmt.Sprintf("/etc/systemd/system/%s", serviceName)
	err := os.WriteFile(servicePath, []byte(serviceContent), 0644)
	if err != nil {
		fmt.Println("Erro ao criar arquivo do serviço:", err)
		return
	}

	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", serviceName).Run()
	err = exec.Command("systemctl", "start", serviceName).Run()
	if err != nil {
		fmt.Println("Erro ao iniciar o serviço:", err)
		return
	}

	servicos[port] = true
	fmt.Printf("Porta %d aberta e serviço %s iniciado.\n", port, serviceName)
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
	err := os.Remove(fmt.Sprintf("/etc/systemd/system/%s", serviceName))
	if err != nil {
		fmt.Println("Erro ao remover serviço:", err)
	}

	exec.Command("systemctl", "daemon-reload").Run()
	delete(servicos, port)
	fmt.Printf("Porta %d fechada e serviço %s removido.\n", port, serviceName)
}

func main() {
	reader := bufio.NewReader(os.Stdin)

	if len(os.Args) != 2 {
		fmt.Println("Uso: ./proxy_worker <porta>")
		os.Exit(1)
	}

	porta := os.Args[1]
	certDir := "/etc/proxyeuro/" + porta
	certFile := certDir + "/cert.pem"
	keyFile := certDir + "/key.pem"

	tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatalf("Erro carregando certificado TLS: %v", err)
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{tlsCert}}

	listener, err := net.Listen("tcp", ":"+porta)
	if err != nil {
		log.Fatalf("Erro ao escutar na porta %s: %v", porta, err)
	}

	log.Printf("🚀 Proxy escutando na porta %s...", porta)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Erro ao aceitar conexão: %v", err)
				continue
			}
			go handleConnection(conn, tlsConfig)
		}
	}()

	// Menu de controle interativo
	for {
		printMenu()
		opt, _ := reader.ReadString('\n')
		opt = strings.TrimSpace(opt)

		switch opt {
		case "1":
			fmt.Print("Digite a porta para abrir: ")
			portStr, _ := reader.ReadString('\n')
			port, _ := strconv.Atoi(strings.TrimSpace(portStr))
			abrirPorta(port)
		case "2":
			fmt.Print("Digite a porta para fechar: ")
			portStr, _ := reader.ReadString('\n')
			port, _ := strconv.Atoi(strings.TrimSpace(portStr))
			fecharPorta(port)
		case "3":
			monitorarPortas()
			fmt.Print("\nPressione Enter para voltar ao menu...")
			reader.ReadString('\n')
		case "4":
			fmt.Println("Encerrando...")
			listener.Close()
			return
		default:
			fmt.Println("Opção inválida.")
		}
	}
}
