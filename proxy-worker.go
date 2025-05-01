#!/bin/bash
set -e

echo "=== Instalando dependências ==="
apt update -y && apt install -y golang git openssl || {
    echo "Erro ao instalar dependências."
    exit 1
}

INSTALL_DIR="/opt/proxyeuro"
REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"

echo "=== Clonando ou atualizando repositório ==="
if [ -d "$INSTALL_DIR/.git" ]; then
    cd "$INSTALL_DIR"
    git pull || { echo "Erro ao atualizar repositório"; exit 1; }
else
    rm -rf "$INSTALL_DIR"
    git clone "$REPO_URL" "$INSTALL_DIR" || { echo "Erro ao clonar repositório"; exit 1; }
fi

cd "$INSTALL_DIR" || exit 1

echo "=== Gerando certificados TLS autoassinados ==="
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365 -nodes -subj "/CN=localhost" || {
    echo "Erro ao gerar certificados."
    exit 1
}

echo "=== Inicializando módulo Go ==="
if [ ! -f go.mod ]; then
    go mod init proxyeuro || {
        echo "Erro ao iniciar módulo Go."
        exit 1
    }
fi

echo "=== Rodando go mod tidy ==="
go mod tidy || {
    echo "Erro ao executar go mod tidy"
    exit 1
}

echo "=== Compilando proxy_worker ==="
go build -o /usr/local/bin/proxy_worker proxy-worker.go || {
    echo "Erro ao compilar proxy_worker.go"
    exit 1
}

echo "=== Compilando proxy_manager como proxyeuro ==="
go build -o /usr/local/bin/proxyeuro proxy-manager.go || {
    echo "Erro ao compilar proxy_manager.go"
    exit 1
}

echo "=== Instalação concluída com sucesso ==="
echo "Use o comando: proxyeuro"