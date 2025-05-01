#!/bin/bash
set -e

echo "=== Instalando dependências ==="
apt update
apt install -y golang git openssl

echo "=== Instalando ou atualizando código-fonte ==="
INSTALL_DIR="/opt/proxyeuro"

if [ -d "$INSTALL_DIR/.git" ]; then
    echo "Diretório já existe. Atualizando..."
    cd "$INSTALL_DIR"
    git pull
else
    echo "Clonando repositório..."
    rm -rf "$INSTALL_DIR"
    git clone https://github.com/jeanfraga33/proxy-go2.git "$INSTALL_DIR"
fi

cd "$INSTALL_DIR"

echo "=== Configurando módulo Go ==="
go mod init proxyeuro || echo "Módulo já existe"
go mod tidy

echo "=== Gerando certificados TLS autoassinados ==="
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 700 -nodes -subj "/CN=localhost"

echo "=== Compilando proxy_manager ==="
go build -o /usr/local/bin/proxyeuro

echo "=== Limpando cache DNS ==="
systemd-resolve --flush-caches || resolvectl flush-caches || echo "Não foi possível limpar o cache DNS"

echo "=== Instalação concluída com sucesso ==="
echo "Use o comando 'proxyeuro' para iniciar"
echo " Proxy Versão 1.0"