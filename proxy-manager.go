package main

import (
	"bufio"
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

	"crypto/tls"
)

const (
	logFilePath = "/var/log/proxyws.log"
	pidFileDir  = "/var/run"
	serviceDir  = "/etc/systemd/system"
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

// peekConn allows putting back one byte into the stream (it will be read first by Read calls)
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

// handleConn manages incoming connections on one port - multiplex WebSocket (WS/WSS), SOCKS5 on same port.
// Sends required HTTP responses (101 upgrade for WS, 200 for SOCKS5).
// Redirects connection bidirectionally to localhost:22 SSH.
func handleConn(conn net.Conn) {
	defer conn.Close()

	clientConn := conn
	isTLS := false

	// Detect TLS by peeking first byte
	peek := make([]byte, 1)
	n, err := clientConn.Read(peek)
	if err != nil {
		logMessage(fmt.Sprintf("Erro na leitura inicial: %v", err))
		return
	}

	if n == 1 && peek[0] == 0x16 && sslConfig != nil {
		// TLS detected, do handshake
		tlsConn := tls.Server(&peekConn{Conn: clientConn, peeked: peek}, sslConfig)
		if err := tlsConn.Handshake(); err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS: %v", err))
			return
		}
		clientConn = tlsConn
		isTLS = true
	} else {
		// Not TLS, restore byte to stream
		clientConn = &peekConn{Conn: clientConn, peeked: peek}
	}

	reader := bufio.NewReader(clientConn)

	// Read first line or byte for protocol detection
	line, err := reader.ReadString('\n')
	if err != nil {
		logMessage(fmt.Sprintf("Erro lendo primeira linha: %v", err))
		return
	}
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "GET") || strings.HasPrefix(line, "CONNECT") {
		// HTTP/WebSocket, parse headers to confirm WebSocket upgrade
		headers := make(map[string]string)
		for {
			hline, err := reader.ReadString('\n')
			if err != nil {
				logMessage(fmt.Sprintf("Erro lendo headers: %v", err))
				return
			}
			hline = strings.TrimSpace(hline)
			if hline == "" {
				break
			}
			parts := strings.SplitN(hline, ":", 2)
			if len(parts) == 2 {
				headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
			}
		}
		if strings.ToLower(headers["upgrade"]) == "websocket" {
			// Upgrade to websocket
			resp := "HTTP/1.1 101 Proxy Cloud JF\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
			if _, err := clientConn.Write([]byte(resp)); err != nil {
				logMessage("Erro enviando resposta 101 websocket: " + err.Error())
				return
			}
			logMessage(fmt.Sprintf("Conexão WebSocket %s estabelecida", map[bool]string{true:"TLS", false:"sem TLS"}[isTLS]))
			sshRedirect(clientConn)
			return
		} else {
			// Non websocket http request, respond 200 OK
			resp := "HTTP/1.1 200 Proxy Cloud JF\r\n\r\n"
			if _, err := clientConn.Write([]byte(resp)); err != nil {
				logMessage("Erro enviando resposta 200 HTTP: " + err.Error())
			}
			logMessage("Requisição HTTP normal com resposta 200 enviada. Fechando conexão.")
			return
		}

	} else if line == "\x05" {
		// SOCKS5 connection (first byte 0x05)
		resp := "HTTP/1.1 200 Proxy Cloud JF\r\n\r\n"
		if _, err := clientConn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 SOCKS5: " + err.Error())
			return
		}
		logMessage("Conexão SOCKS5 estabelecida, enviando proxy para SSH")
		sshRedirect(clientConn)
		return
	} else {
		// Indefinido, responder 200 para matar conexão educadamente
		resp := "HTTP/1.1 200 Proxy Cloud JF\r\n\r\n"
		clientConn.Write([]byte(resp))
		logMessage("Conexão recebida com protocolo desconhecido, respondeu 200 e fechou")
	}
}

// sshRedirect conecta na porta 22 local e encaminha dados bidirecionalmente.
func sshRedirect(clientConn net.Conn) {
	sshConn, err := net.Dial("tcp", "127.0.0.1:22")
	if err != nil {
		logMessage("Erro conectando SSH local: " + err.Error())
		return
	}
	defer sshConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	// Copy client->ssh
	go func() {
		defer wg.Done()
		io.Copy(sshConn, clientConn)
	}()

	// Copy ssh->client
	go func() {
		defer wg.Done()
		io.Copy(clientConn, sshConn)
	}()

	wg.Wait()
	logMessage("Conexão proxy finalizada")
}

// startProxy inicia listener TCP e aceita conexões tratadas em goroutine
func startProxy(port int) {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logMessage(fmt.Sprintf("Erro iniciando listener na porta %d: %v", port, err))
		return
	}
	defer listener.Close()

	// Escrever PID para controle
	pidFile := fmt.Sprintf("%s/proxyeuro_%d.pid", pidFileDir, port)
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		logMessage(fmt.Sprintf("Falha ao gravar PID file: %v", err))
	}

	logMessage(fmt.Sprintf("Proxy iniciado na porta %d", port))

	// Captura de sinais para desligar proxy corretamente
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
			// listener aceitando conexões, se erro fechar, encerra loop.
			logMessage(fmt.Sprintf("Erro aceitando conexão na porta %d: %v", port, err))
			break
		}
		go handleConn(conn)
	}
	logMessage(fmt.Sprintf("Proxy encerrado na porta %d", port))
	os.Remove(pidFile)
}

// systemdServicePath retorna o path do serviço systemd para a porta
func systemdServicePath(port int) string {
	return fmt.Sprintf("%s/proxyeuro@%d.service", serviceDir, port)
}

// createSystemdService cria arquivo de serviço systemd para a porta
func createSystemdService(port int, execPath string) error {
	serviceContent := fmt.Sprintf(`[Unit]
Description=ProxyEuro na porta %d
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

// enableAndStartService habilita e inicia serviço systemd
func enableAndStartService(port int) error {
	serviceName := fmt.Sprintf("proxyeuro@%d.service", port)
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

// stopAndDisableService para e desabilita o serviço systemd da porta
func stopAndDisableService(port int) error {
	serviceName := fmt.Sprintf("proxyeuro@%d.service", port)
	cmd := exec.Command("systemctl", "stop", serviceName)
	cmd.Run() // Ignorar erro para continuar
	cmd = exec.Command("systemctl", "disable", serviceName)
	cmd.Run()
	return os.Remove(systemdServicePath(port))
}

// clearScreen limpa terminal para todos OSs suportados simples
func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func main() {
	if len(os.Args) > 1 {
		// Se argumento for número, executa proxy na porta - para execução via systemd
		port, err := strconv.Atoi(os.Args[1])
		if err != nil {
			fmt.Printf("Parâmetro inválido: %s\n", os.Args[1])
			return
		}

		// Configurar TLS se possível (carregar cert.pem/key.pem na pasta)
		cert, err := tls.LoadX509KeyPair("cert.pem", "key.pem")
		if err == nil {
			sslConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		} else {
			logMessage("Certificados TLS não carregados, executando sem TLS")
		}

		startProxy(port)
		return
	}

	// Menu de interação para abrir/fechar portas e sair
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
			// Criar service systemd e iniciar
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
