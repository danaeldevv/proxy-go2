// proxy-worker.go atualizado
package main

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

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

func handleConnection(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		log.Println("Erro ao ler dados iniciais:", err)
		return
	}
	data := buf[:n]

	switch {
	case isSOCKS5(data):
		log.Println("Conexão SOCKS5 detectada")
		handleSOCKS5(conn, data)
	case isWebSocket(data):
		log.Println("Conexão WebSocket detectada")
		handleWebSocket(conn, data)
	case isHTTP101(data) || isHTTP200(data):
		log.Println("Conexão HTTP detectada")
		conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nProxyEuro HTTP ativo\n"))
	default:
		log.Println("Protocolo desconhecido")
		conn.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Type: text/plain\r\n\r\nProtocolo não suportado\n"))
	}
}

func handleSOCKS5(conn net.Conn, data []byte) {
	conn.Write([]byte{0x05, 0x00}) // resposta para método no-auth
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil || n < 7 {
		return
	}
	// Apenas responde como se conectasse com sucesso
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0x00, 0x16})
}

func handleWebSocket(conn net.Conn, data []byte) {
	sshConn, err := net.Dial("tcp", "127.0.0.1:22")
	if err != nil {
		log.Println("Erro ao conectar ao SSH:", err)
		return
	}
	defer sshConn.Close()

	sshConn.Write(data)
	go io.Copy(sshConn, conn)
	io.Copy(conn, sshConn)
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Uso: %s <porta>", os.Args[0])
	}
	porta := os.Args[1]

	certDir := os.Getenv("CERT_DIR")
	if certDir == "" {
		certDir = "/opt/proxyeuro/certs"
	}
	certPath := certDir + "/cert.pem"
	keyPath := certDir + "/key.pem"

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		log.Fatalf("Erro carregando certificado TLS: %v", err)
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
	listener, err := tls.Listen("tcp", ":"+porta, tlsConfig)
	if err != nil {
		log.Fatalf("Erro ao escutar na porta %s: %v", porta, err)
	}
	defer listener.Close()

	log.Printf("ProxyEuro escutando na porta %s...", porta)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Erro ao aceitar conexão:", err)
			continue
		}
		go handleConnection(conn)
	}
}
