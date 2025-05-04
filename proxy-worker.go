package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
)

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

func main() {
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
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Erro ao aceitar conexão: %v", err)
			continue
		}
		go handleConnection(conn, tlsConfig)
	}
}
