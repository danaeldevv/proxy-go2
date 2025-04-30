#!/bin/bash

set -e

echo "=== Instalando dependências ==="
if ! command -v go &> /dev/null; then
    echo "Go não encontrado. Instalando..."
    apt update && apt install -y golang
else
    echo "Go já está instalado."
fi

echo "=== Criando diretório do proxy ==="
mkdir -p /opt/proxygerenciador
cp proxy_worker.go /opt/proxygerenciador/
cp proxy_manager.go /opt/proxygerenciador/

echo "=== Gerando certificados TLS autoassinados ==="
cd /opt/proxygerenciador
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365 -nodes -subj "/CN=localhost"

echo "=== Compilando proxy_manager ==="
go build -o /usr/local/bin/proxygerenciador proxy_manager.go

echo "=== Limpando arquivos temporários ==="
rm -f /opt/proxygerenciador/proxy_manager.go

echo "=== Limpando cache DNS ==="
systemd-resolve --flush-caches || resolvectl flush-caches || echo "Não foi possível limpar o cache DNS"

echo "=== Instalado com sucesso! Use o comando 'proxygerenciador' para iniciar ==="
