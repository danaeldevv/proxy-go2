package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

func main() {
	reader := bufio.NewReader(os.Stdin)
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
			return
		default:
			fmt.Println("Opção inválida.")
		}
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
