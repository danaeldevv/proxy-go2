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

func handleConn(conn net.Conn) {
	defer conn.Close()

	clientConn := conn
	isTLS := false

	peek := make([]byte, 1)
	n, err := clientConn.Read(peek)
	if err != nil {
		logMessage(fmt.Sprintf("Erro na leitura inicial: %v", err))
		return
	}

	if n == 1 && peek[0] == 0x16 && sslConfig != nil {
		tlsConn := tls.Server(&peekConn{Conn: clientConn, peeked: peek}, sslConfig)
		if err := tlsConn.Handshake(); err != nil {
			logMessage(fmt.Sprintf("Erro handshake TLS: %v", err))
			return
		}
		clientConn = tlsConn
		isTLS = true
	} else {
		clientConn = &peekConn{Conn: clientConn, peeked: peek}
	}

	reader := bufio.NewReader(clientConn)

	line, err := reader.ReadString('\n')
	if err != nil {
		logMessage(fmt.Sprintf("Erro lendo primeira linha: %v", err))
		// Mesmo em erro tentamos redirecionar para SSH porque usuário quer qualquer conexão
		sshRedirect(clientConn)
		return
	}
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "GET") || strings.HasPrefix(line, "CONNECT") {
		headers := make(map[string]string)
		for {
			hline, err := reader.ReadString('\n')
			if err != nil {
				logMessage(fmt.Sprintf("Erro lendo headers: %v", err))
				break // não encerra conexão pra continuar proxy
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
			resp := "HTTP/1.1 101 Proxy Cloud JF\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
			if _, err := clientConn.Write([]byte(resp)); err != nil {
				logMessage("Erro enviando resposta 101 websocket: " + err.Error())
				return
			}
			logMessage(fmt.Sprintf("Conexão WebSocket %s estabelecida", map[bool]string{true:"TLS", false:"sem TLS"}[isTLS]))
			sshRedirect(clientConn)
			return
		} else {
			resp := "HTTP/1.1 200 Proxy Cloud JF\r\n\r\n"
			if _, err := clientConn.Write([]byte(resp)); err != nil {
				logMessage("Erro enviando resposta 200 HTTP: " + err.Error())
			}
			logMessage("HTTP normal identificado, resposta 200 enviada")
			sshRedirect(clientConn)
			return
		}
	} else if line == "\x05" {
		resp := "HTTP/1.1 200 Proxy Cloud JF\r\n\r\n"
		if _, err := clientConn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 SOCKS5: " + err.Error())
			return
		}
		logMessage("Conexão SOCKS5 estabelecida, enviando proxy para SSH")
		sshRedirect(clientConn)
		return
	} else {
		// Nenhum protocolo reconhecido, mas redireciona para SSH conforme pedido
		resp := "HTTP/1.1 200 Proxy Cloud JF\r\n\r\n"
		if _, err := clientConn.Write([]byte(resp)); err != nil {
			logMessage("Erro enviando resposta 200 padrão: " + err.Error())
		}
		logMessage("Protocolo desconhecido, respondeu 200 e redirecionando para SSH")
		sshRedirect(clientConn)
	}
}

// sshRedirect forward client <-> SSH listening on 127.0.0.1:22
func sshRedirect(clientConn net.Conn) {
	sshConn, err := net.Dial("tcp", "127.0.0.1:22")
	if err != nil {
		logMessage("Erro conectando SSH local: " + err.Error())
		return
	}
	defer sshConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(sshConn, clientConn)
	}()
	go func() {
		defer wg.Done()
		io.Copy(clientConn, sshConn)
	}()
	wg.Wait()

	logMessage("Conexão proxy finalizada")
}

func startProxy(port int) {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logMessage(fmt.Sprintf("Erro iniciando listener na porta %d: %v", port, err))
		return
	}
	defer listener.Close()

	pidFile := fmt.Sprintf("%s/proxyeuro_%d.pid", pidFileDir, port)
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
		go handleConn(conn)
	}
	logMessage(fmt.Sprintf("Proxy encerrado na porta %d", port))
	os.Remove(pidFile)
}

func systemdServicePath(port int) string {
	return fmt.Sprintf("%s/proxyeuro@%d.service", serviceDir, port)
}

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

func stopAndDisableService(port int) error {
	serviceName := fmt.Sprintf("proxyeuro@%d.service", port)
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
