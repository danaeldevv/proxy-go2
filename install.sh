#!/bin/bash
set -e

echo "=== Instalando dependências ==="
if ! command -v go &> /dev/null; then
    echo "Go não encontrado. Instalando..."
    apt update && apt install -y golang
else
    echo "dependencia já está instalada."
fi

echo "=== instalando ou atualizando ==="
INSTALL_DIR="/opt/proxyeuro

if [ -d "$INSTALL_DIR" ]; then
    echo "Diretório já existe. Atualizando..."
    cd "$INSTALL_DIR"
    git pull
else
    git clone https://github.com/jeanfraga33/proxy-go2.git "$INSTALL_DIR"
fi

echo "=== Gerando certificados TLS autoassinados ==="
cd "$INSTALL_DIR"
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 700 -nodes -subj "/CN=localhost"

echo "=== Compilando proxy ==="
go build -o /usr/local/bin/proxyeuro proxy_manager.go

echo "=== Limpando cache DNS ==="
systemd-resolve --flush-caches || resolvectl flush-caches || echo "Não foi possível limpar o cache DNS"

echo "=== Instalação concluída com sucesso ==="
echo "Use o comando 'proxyeuro' para iniciar"
echo " Proxy Verção 1.0"
