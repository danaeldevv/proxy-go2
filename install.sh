#!/bin/bash

set -e

# URL do código fonte C++ do proxy
PROXY_REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
PROXY_DIR="proxy-go"
EXEC_NAME="proxy-manager"

echo "Iniciando instalação do Proxy C++ com Boost.Asio..."

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
}

install_dependencies() {
    echo "Atualizando repositórios e instalando dependências..."
    sudo apt-get update -y
    sudo apt-get install -y build-essential libssl-dev libboost-system-dev libboost-thread-dev git pkg-config cmake curl
    echo "Dependências instaladas."
}

clone_repo() {
    echo "Clonando repositório do proxy..."
    git clone "$PROXY_REPO_URL" "$PROXY_DIR"
}

patch_code_for_boost_asio() {
    echo "Ajustando includes para Boost.Asio..."
    # Substitui #include <asio.hpp> por #include <boost/asio.hpp> e #include <boost/asio/ssl.hpp>
    sed -i 's/#include <asio.hpp>/#include <boost\/asio.hpp>/' "$PROXY_DIR/proxy-manager.cpp"
    sed -i 's/#include <asio\/ssl.hpp>/#include <boost\/asio\/ssl.hpp>/' "$PROXY_DIR/proxy-manager.cpp"
    echo "Includes ajustados."
}

build_proxy() {
    echo "Compilando proxy..."
    cd "$PROXY_DIR"
    g++ proxy-manager.cpp -o $EXEC_NAME -lboost_system -lboost_thread -lpthread -lssl -lcrypto
    if [ ! -f "$EXEC_NAME" ]; then
        echo "Erro: compilação falhou, executável não criado."
        exit 1
    fi
    cd ..
    echo "Compilação concluída."
}

install_proxy() {
    echo "Instalando proxy no sistema..."
    sudo cp "$PROXY_DIR/$EXEC_NAME" /usr/local/bin/
    sudo chmod +x /usr/local/bin/$EXEC_NAME
    echo "Proxy instalado em /usr/local/bin/$EXEC_NAME"
}

cleanup() {
    echo "Limpando arquivos temporários..."
    rm -rf "$PROXY_DIR"
    echo "Diretórios temporários removidos."
    echo "Limpando cache DNS..."
    if systemctl is-active --quiet systemd-resolved; then
        sudo systemctl restart systemd-resolved
        echo "Cache DNS reiniciado via systemd-resolved."
    else
        sudo /etc/init.d/dns-clean restart || echo "Falha ao limpar cache DNS via dns-clean"
    fi
}

clean_previous_install
install_dependencies
clone_repo
patch_code_for_boost_asio
build_proxy
install_proxy
cleanup

echo "Instalação concluída com sucesso!"
echo "Use o comando '$EXEC_NAME' para rodar o proxy."
``` ⬤
