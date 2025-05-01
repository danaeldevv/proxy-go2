package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type ProxyProcess struct {
	Port int
	Cmd  *exec.Cmd
}

var (
	processes = make(map[int]*ProxyProcess)
	mutex     sync.Mutex
)

func main() {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Println("\n==== MENU Proxy Euro ====")
		fmt.Println("1. Abrir porta")
		fmt.Println("2. Fechar porta")
		fmt.Println("3. Monitorar portas abertas")
		fmt.Println("4. Sair")
		fmt.Print("Escolha: ")
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
		}
	}
}

func abrirPorta(port int) {
	mutex.Lock()
	defer mutex.Unlock()
	if _, exists := processes[port]; exists {
		fmt.Println("Porta já está aberta")
		return
	}

	cmd := exec.Command("/usr/local/bin/proxy_worker", strconv.Itoa(port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	err := cmd.Start()
	if err != nil {
		fmt.Println("Erro ao abrir porta:", err)
		return
	}

	processes[port] = &ProxyProcess{Port: port, Cmd: cmd}
	fmt.Println("Porta", port, "aberta com PID", cmd.Process.Pid)
}

func fecharPorta(port int) {
	mutex.Lock()
	defer mutex.Unlock()
	proc, exists := processes[port]
	if !exists {
		fmt.Println("Porta não encontrada.")
		return
	}
	proc.Cmd.Process.Kill()
	delete(processes, port)
	fmt.Println("Porta", port, "fechada.")
}

func monitorarPortas() {
	mutex.Lock()
	defer mutex.Unlock()
	if len(processes) == 0 {
		fmt.Println("Nenhuma porta aberta.")
		return
	}
	for port := range processes {
		fmt.Printf("Porta %d está ativa.\n", port)
	}
}