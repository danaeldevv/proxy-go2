#!/bin/bash

# Nome do script Python
SCRIPT_NAME="proxy-py.py"
SCRIPT_PATH="/usr/local/bin/proxy_server.py"
REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
REPO_DIR="proxy-go2"

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

# Remove o diretório do repositório clonado para evitar conflitos
remove_existing_repo() {
    if [ -d "$REPO_DIR" ]; then
        echo "Removendo diretório antigo do repositório '$REPO_DIR'..."
        rm -rf "$REPO_DIR"
        echo "Diretório antigo removido."
    fi
}

# Função para instalar Git e outras dependências
install_dependencies() {
    echo "Instalando Git, Python3 e dependências..."
    sudo apt-get update
    sudo apt-get install -y git python3 python3-pip
    pip3 install --upgrade websockets
    echo "Git e dependências instaladas."
}

# Função para clonar o repositório do GitHub
clone_repository() {
    echo "Clonando o repositório do GitHub..."
    git clone "$REPO_URL"
    if [ $? -ne 0 ]; then
        echo "Erro ao clonar o repositório. Abortando instalação."
        exit 1
    fi
}

# Função para copiar o script para o local final e configurar permissões
install_script() {
    if [ -f "$REPO_DIR/$SCRIPT_NAME" ]; then
        sudo cp "$REPO_DIR/$SCRIPT_NAME" "$SCRIPT_PATH"
        sudo chmod +x "$SCRIPT_PATH"
        echo "Script instalado em $SCRIPT_PATH."
    else
        echo "Erro: O arquivo '$SCRIPT_NAME' não foi encontrado no repositório clonado."
        exit 1
    fi
}

# Função principal
main() {
    remove_previous_installation
    remove_existing_repo
    install_dependencies
    clone_repository
    install_script
    echo ""
    echo "Instalação concluída."
    echo "Você pode executar o proxy com o comando:"
    echo "  python3 $SCRIPT_PATH"
    echo ""
    echo "Ou, para executar diretamente (dependendo do shebang do script):"
    echo "  $SCRIPT_PATH"
}

# Executa a função principal
main
