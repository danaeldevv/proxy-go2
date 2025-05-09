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
	"net/http"
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
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)
	memMB = float64(memStats.Alloc) / (1024 * 1024)
	cpu = 0.0 // Placeholder for CPU usage
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
		runProxy(*flagPort)
		return
	}

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

	if !isWritable(systemdDir) {
		fmt.Printf("Erro: sem permissão para escrever em %s. Execute como root.\n", systemdDir)
		time.Sleep(3 * time.Second)
		return
	}

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

	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", fmt.Sprintf("%s%d.service", servicePrefix, port)).Run()
	exec.Command("systemctl", "start", fmt.Sprintf("%s%d.service", servicePrefix, port)).Run()

	fmt.Printf("Porta %d aberta com serviço systemd criado e iniciado.\n", port)
	fmt.Println("Aperte Enter para continuar...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

// closePort asks user for port and stops that systemd service and removes the unit file
func
