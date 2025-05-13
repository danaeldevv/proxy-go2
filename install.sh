#!/bin/bash

# Nome do script Python
SCRIPT_NAME="proxy_server.py"
SCRIPT_PATH="/usr/local/bin/$SCRIPT_NAME"

# Função para remover instalações anteriores
remove_previous_installation() {
    echo "Removendo instalações anteriores..."
    if [ -f "$SCRIPT_PATH" ]; then
        rm "$SCRIPT_PATH"
        echo "Instalação anterior removida."
    else
        echo "Nenhuma instalação anterior encontrada."
    fi
}

# Função para instalar Git e outras dependências
install_dependencies() {
    echo "Instalando Git e dependências..."
    sudo apt-get update
    sudo apt-get install -y git python3 python3-pip
    pip3 install websockets
    echo "Git e dependências instaladas."
}

# Função para clonar o repositório do GitHub
clone_repository() {
    echo "Clonando o repositório do GitHub..."
    git clone https://github.com/jeanfraga33/proxy-go2.git
    cp "proxy-go2/$SCRIPT_NAME" "$SCRIPT_PATH"
    chmod +x "$SCRIPT_PATH"
    echo "Script instalado em $SCRIPT_PATH."
}

# Função principal
main() {
    remove_previous_installation
    install_dependencies
    clone_repository
    echo "Instalação concluída. Você pode executar o proxy usando o comando: $SCRIPT_NAME"
}

# Executa a função principal
main
