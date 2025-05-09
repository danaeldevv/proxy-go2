#!/bin/bash

set -e

# URL do código fonte C++ do proxy
PROXY_REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
PROXY_DIR="proxy-go"
EXEC_NAME="proxy-manager"

echo "Iniciando instalação do Proxy C++..."

# Função para limpar instalações anteriores
clean_previous_install() {
    echo "Removendo instalações anteriores..."
    if command -v $EXEC_NAME &> /dev/null; then
        sudo rm -f "$(command -v $EXEC_NAME)"
        echo "Executável antigo removido."
    fi
    if [ -d "$PROXY_DIR" ]; then
        rm -rf "$PROXY_DIR"
        echo "Diretório antigo removido."
    fi
    if [ -d "libevent-2.1.12-stable" ]; then
        rm -rf libevent-2.1.12-stable
        echo "Pasta libevent removida."
    fi
}

# Instalar dependências necessárias do sistema
install_dependencies() {
    echo "Atualizando repositórios..."
    sudo apt-get update -y

    echo "Instalando dependências de build e OpenSSL..."
    sudo apt-get install -y build-essential libssl-dev git pkg-config cmake curl

    # Tenta instalar libevent-dev via gerenciador de pacotes
    if ! dpkg -s libevent-dev >/dev/null 2>&1; then
        echo "libevent-dev não encontrado, será instalado manualmente."
        install_libevent_manual
    else
        echo "libevent-dev instalado via apt."
    fi
}

# Instalação manual do libevent se não estiver disponível
install_libevent_manual() {
    echo "Instalando libevent manualmente..."
    wget -c https://libevent.org/downloads/libevent-2.1.12-stable.tar.gz
    tar -xzf libevent-2.1.12-stable.tar.gz
    cd libevent-2.1.12-stable
    ./configure
    make
    sudo make install
    cd ..
    rm libevent-2.1.12-stable.tar.gz
    echo "libevent instalado manualmente."
}

# Clonar repositório do proxy C++
clone_repo() {
    echo "Clonando repositório do proxy..."
    git clone "$PROXY_REPO_URL" "$PROXY_DIR"
}

# Compilar proxy
build_proxy() {
    echo "Compilando proxy..."
    cd "$PROXY_DIR"

    # Ajuste o include para usar o caminho correto do libevent instalado manualmente (/usr/local/include/event2)
    g++ proxy-manager.cpp -o $EXEC_NAME -I/usr/local/include/event2 -lssl -lcrypto -levent -lpthread

    if [ ! -f "$EXEC_NAME" ]; then
        echo "Erro: compilação falhou, executável não criado."
        exit 1
    fi

    cd ..
    echo "Compilação concluída."
}

# Instalar executável no sistema
install_proxy() {
    echo "Instalando proxy no sistema..."
    sudo cp "$PROXY_DIR/$EXEC_NAME" /usr/local/bin/
    sudo chmod +x /usr/local/bin/$EXEC_NAME
    echo "Proxy instalado em /usr/local/bin/$EXEC_NAME"
}

# Limpar arquivos temporários/repositorio e cache DNS
cleanup() {
    echo "Limpando arquivos temporários..."
    rm -rf "$PROXY_DIR"
    rm -rf libevent-2.1.12-stable
    echo "Diretórios temporários removidos."

    echo "Limpando cache DNS..."
    if systemctl is-active --quiet systemd-resolved; then
        sudo systemctl restart systemd-resolved
        echo "Cache DNS reiniciado via systemd-resolved."
    else
        sudo /etc/init.d/dns-clean restart || echo "Falha ao limpar cache DNS via dns-clean"
    fi
}

# Fluxo completo da instalação
clean_previous_install
install_dependencies
clone_repo
build_proxy
install_proxy
cleanup

echo "Instalação concluída com sucesso!"
echo "Use o comando '$EXEC_NAME' para rodar o proxy."
