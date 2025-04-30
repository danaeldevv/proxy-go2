#!/bin/bash

set -e

echo "=== Instalando dependências ==="
if ! command -v go &> /dev/null; then
    echo "não encontrado a dependencia. Instalando..."
    apt update && apt install -y golang
else
    echo "uma das dependencias já está instalado."
fi

echo "=== Criando diretório do proxy ==="
mkdir -p /opt/proxyeuro
cd /opt/proxyeuro

echo "=== Baixando códigos fonte do Proxy ==="
curl -sSL https://raw.githubusercontent.com/jeanfraga33/proxy-go2/main/proxy-worker.go -o proxy_worker.go
curl -sSL https://raw.githubusercontent.com/jeanfraga33/proxy-go2/main/proxy-manager.go -o proxy_manager.go

echo "=== Gerando certificados TLS autoassinados ==="
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 780 -nodes -subj "/CN=localhost"

echo "=== Compilando proxy ==="
go build -o /usr/local/bin/proxyeuro proxy_manager.go

echo "=== Limpando arquivos temporários ==="
rm -f proxy_manager.go

echo "=== Limpando cache DNS ==="
systemd-resolve --flush-caches || resolvectl flush-caches || echo "Não foi possível limpar o cache DNS"

echo "=== Instalado/Atualizado com sucesso! Use o comando 'proxyeuro' para iniciar ==="
echo " Proxy verção 1.0 "
