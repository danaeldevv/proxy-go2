#!/bin/bash

# Definindo variáveis
PROXY_DIR="/usr/local/bin/proxy_worker"
SYSTEMD_SERVICE_PATH="/etc/systemd/system/proxyeuro@.service"
GITHUB_REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
INSTALL_DIR="/usr/local/src/proxy-go2"

# Função para limpar tabela DNS
clean_dns_cache() {
    echo "Limpando a tabela DNS..."
    sudo systemd-resolve --flush-caches
    echo "Tabela DNS limpa."
}

# Verificar se há instalação anterior do proxy
check_existing_installation() {
    if [ -f "$PROXY_DIR" ]; then
        echo "Instalação anterior detectada. Removendo a instalação antiga..."
        sudo systemctl stop proxyeuro@*
        sudo systemctl disable proxyeuro@*
        sudo rm -rf /etc/systemd/system/proxyeuro@*.service
        sudo rm -f "$PROXY_DIR"
        echo "Instalação antiga removida."
    fi
}

# Instalar dependências
install_dependencies() {
    echo "Instalando dependências..."
    sudo apt install -y golang-go git build-essential
    echo "Dependências instaladas."
}

# Baixar o repositório e compilar o proxy
install_proxy() {
    echo "Baixando o código do repositório do GitHub..."
    git clone "$GITHUB_REPO_URL" "$INSTALL_DIR"
    cd "$INSTALL_DIR" || exit

    echo "Compilando o proxy..."
    go build -o "$PROXY_DIR" ./proxy-manager.go
    sudo chmod +x "$PROXY_DIR"
    echo "Proxy compilado e instalado."
}

# Criar o serviço systemd para o proxy
create_systemd_service() {
    echo "Criando serviço systemd para o proxy..."
    sudo cp "$INSTALL_DIR/proxyeuro@.service" "$SYSTEMD_SERVICE_PATH"
    sudo systemctl daemon-reload
    echo "Serviço systemd criado."
}

# Criar link simbólico para o comando 'proxyeuro'
create_symlink() {
    echo "Criando link simbólico para o comando proxyeuro..."
    sudo ln -s "$PROXY_DIR" /usr/local/bin/proxyeuro
    echo "Link simbólico criado. Agora você pode usar o comando 'proxyeuro'."
}

# Exibir instrução para abrir o proxy
show_instructions() {
    echo "Instalação concluída com sucesso!"
    echo "Para abrir o proxy, use o comando:"
    echo "proxyeuro"
    echo "Você pode iniciar o proxy para uma porta específica com o comando:"
    echo "proxyeuro [PORTA]"
    echo "Exemplo: proxyeuro 8080"
}

# Função principal
main() {
    # Passo 1: Verificar e remover instalação anterior
    check_existing_installation

    # Passo 2: Instalar dependências
    install_dependencies

    # Passo 3: Baixar e instalar o proxy
    install_proxy

    # Passo 4: Criar o serviço systemd
    create_systemd_service

    # Passo 5: Criar link simbólico
    create_symlink

    # Passo 6: Limpar tabela DNS
    clean_dns_cache

    # Passo 7: Mostrar instruções para abrir o proxy
    show_instructions
}

# Executar o script
main
