#!/bin/bash

set -e

PROXY_REPO_URL="https://raw.githubusercontent.com/jeanfraga33/proxy-go2/refs/heads/main/proxy-manager.go"   # <-- Substitua pelo URL correto do seu repositório
PROXY_DIR="proxy-cpp"
EXEC_NAME="proxy"

echo "Iniciando instalação do Proxy C++..."

# Função para limpar instalações anteriores
clean_previous_install() {
    echo "Removendo instalações anteriores..."
    if command -v $EXEC_NAME &> /dev/null; then
        sudo rm -f $(command -v $EXEC_NAME)
        echo "Executável antigo removido."
    fi
    if [ -d "$PROXY_DIR" ]; then
        rm -rf "$PROXY_DIR"
        echo "Diretório antigo removido."
    fi
}

# Instalar dependências necessárias
install_dependencies() {
    echo "Instalando dependências..."
    # Atualiza repositórios
    sudo apt-get update -y

    # Instala build-essential, OpenSSL e libevent, git, pkg-config, make, cmake
    sudo apt-get install -y build-essential libssl-dev libevent-dev git pkg-config cmake

    echo "Dependências instaladas."
}

# Clonar ou atualizar repositorio
clone_repo() {
    echo "Clonando repositório..."
    git clone "$PROXY_REPO_URL" "$PROXY_DIR"
}

# Compilar proxy
build_proxy() {
    echo "Compilando proxy..."
    cd "$PROXY_DIR"
    # Assumindo projeto C++ simples sem cmake: compile proxy.cpp com flags libs SSL e event
    g++ proxy.cpp -o $EXEC_NAME -lssl -lcrypto -levent -lpthread

    if [ ! -f "$EXEC_NAME" ]; then
        echo "Erro: compilação falhou, executável não criado."
        exit 1
    fi

    echo "Compilação concluída."
}

# Instalar executável no sistema
install_proxy() {
    echo "Instalando proxy no sistema..."
    sudo cp "$EXEC_NAME" /usr/local/bin/
    sudo chmod +x /usr/local/bin/$EXEC_NAME
    echo "Proxy instalado em /usr/local/bin/$EXEC_NAME"
}

# Limpar arquivos temporários/repositorio e cache DNS
cleanup() {
    echo "Limpando arquivos temporários..."

    cd ..
    rm -rf "$PROXY_DIR"
    echo "Diretório temporário removido."

    echo "Limpando cache DNS..."
    if systemctl is-active --quiet systemd-resolved; then
        sudo systemctl restart systemd-resolved
        echo "Cache DNS reiniciado via systemd-resolved."
    else
        # fallback para resolverd
        sudo /etc/init.d/dns-clean restart || echo "Falha ao limpar cache DNS via dns-clean"
    fi
}

# Fluxo completo
clean_previous_install
install_dependencies
clone_repo
build_proxy
install_proxy
cleanup

echo "Instalação concluída com sucesso!"
echo "Use o comando '$EXEC_NAME' para rodar o proxy."

``` ⬤
