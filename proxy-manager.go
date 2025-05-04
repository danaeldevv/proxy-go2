/// proxy-manager.go
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	mutex sync.Mutex
)

func clearScreen() {
	cmd := exec.Command("clear")
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func centerText(text string) string {
	cols := 60
	pad := (cols - len(text)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + text
}

func menu() {
	reader := bufio.NewReader(os.Stdin)
	for {
		clearScreen()
		fmt.Println(centerText("=============================="))
		fmt.Println(centerText("      MENU Proxy Euro      "))
		fmt.Println(centerText("=============================="))
		fmt.Println(centerText("1. Abrir porta"))
		fmt.Println(centerText("2. Fechar porta"))
		fmt.Println(centerText("3. Monitorar portas abertas"))
		fmt.Println(centerText("4. Sair"))
		fmt.Print("\nEscolha: ")
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
		case "4":
			fmt.Println("Encerrando...")
			return
		default:
			fmt.Println("Opção inválida.")
			time.Sleep(2 * time.Second)
		}
	}
}

func abrirPorta(port int) {
	mutex.Lock()
	defer mutex.Unlock()

	serviceName := fmt.Sprintf("proxyeuro@%d.service", port)
	serviceFile := fmt.Sprintf("/etc/systemd/system/%s", serviceName)
	
	if _, err := os.Stat(serviceFile); err == nil {
		fmt.Println("Porta já está aberta (serviço existente)")
		time.Sleep(2 * time.Second)
		return
	}

	script := fmt.Sprintf(`[Unit]
Description=Proxy Euro Porta %d
After=network.target

[Service]
ExecStart=/usr/local/bin/proxy_worker %d
Restart=always

[Install]
WantedBy=multi-user.target`, port, port)

	err := os.WriteFile(serviceFile, []byte(script), 0644)
	if err != nil {
		fmt.Println("Erro ao criar service:", err)
		return
	}

	exec.Command("systemctl", "daemon-reexec").Run()
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", serviceName).Run()
	exec.Command("systemctl", "start", serviceName).Run()

	fmt.Printf("Porta %d aberta via systemd!\n", port)
	time.Sleep(2 * time.Second)
}

func fecharPorta(port int) {
	mutex.Lock()
	defer mutex.Unlock()
	serviceName := fmt.Sprintf("proxyeuro@%d.service", port)
	serviceFile := fmt.Sprintf("/etc/systemd/system/%s", serviceName)

	exec.Command("systemctl", "stop", serviceName).Run()
	exec.Command("systemctl", "disable", serviceName).Run()
	os.Remove(serviceFile)
	exec.Command("systemctl", "daemon-reload").Run()
	fmt.Printf("Porta %d fechada e serviço removido.\n", port)
	time.Sleep(2 * time.Second)
}

func monitorarPortas() {
	mutex.Lock()
	defer mutex.Unlock()
	fmt.Println("Portas abertas:")
	out, err := exec.Command("systemctl", "list-units", "--type=service", "--state=running").Output()
	if err != nil {
		fmt.Println("Erro ao listar serviços.")
	} else {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			if strings.Contains(line, "proxyeuro@") {
				fmt.Println(line)
			}
		}
	}
	fmt.Print("Pressione ENTER para voltar...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func main() {
	menu()
}
