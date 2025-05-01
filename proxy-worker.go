package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Uso: proxy_worker <porta>")
		os.Exit(1)
	}
	port := os.Args[1]
	startProxy(port)
}

func startProxy(port string) {
	addr := ":" + port

	cert, err := tls.LoadX509KeyPair("cert.pem", "key.pem")
	if err != nil {
		log.Fatal("Erro carregando certificado TLS:", err)
	}

	config := &tls.Config{Certificates: []tls.Certificate{cert}}
	listener, err := tls.Listen("tcp", addr, config)
	if err != nil {
		log.Fatal("Erro ao escutar porta:", err)
	}
	defer listener.Close()
	fmt.Println("Proxy escutando em:", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("Erro de conexão:", err)
			continue
		}
		go manipularConexao(conn)
	}
}

func manipularConexao(client net.Conn) {
	defer client.Close()

	head := make([]byte, 1024)
	n, err := client.Read(head)
	if err != nil {
		log.Println("Erro lendo cabeçalho:", err)
		return
	}

	data := head[:n]

	if isHTTP101(data) || isHTTP200(data) || isWebSocket(data) {
		log.Println("Conexão HTTP/WebSocket detectada")
	} else if isSOCKS5(data) {
		log.Println("Conexão SOCKS5 detectada")
	} else {
		log.Println("Protocolo desconhecido")
		return
	}

	sshConn, err := net.Dial("tcp", "127.0.0.1:22")
	if err != nil {
		log.Println("Erro conectando ao SSH local:", err)
		return
	}
	defer sshConn.Close()

	sshConn.Write(data)
	go io.Copy(sshConn, client)
	io.Copy(client, sshConn)
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