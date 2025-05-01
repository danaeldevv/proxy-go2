#!/bin/bash
set -e

echo "=== Instalando dependências ==="
apt update
apt install -y golang git openssl

echo "=== Removendo instalação anterior ==="
rm -rf /opt/proxyeuro
mkdir -p /opt/proxyeuro
cd /opt/proxyeuro

echo "=== Baixando arquivos do GitHub ==="
wget -q https://raw.githubusercontent.com/jeanfraga33/proxy-go2/main/proxy-manager.go -O proxy-manager.go || { echo "Erro ao baixar proxy-manager.go"; exit 1; }
wget -q https://raw.githubusercontent.com/jeanfraga33/proxy-go2/main/proxy-worker.go -O proxy-worker.go || { echo "Erro ao baixar proxy-worker.go"; exit 1; }

echo "=== Gerando certificados TLS ==="
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 700 -nodes -subj "/CN=localhost" || { echo "Erro ao gerar certificados TLS"; exit 1; }

echo "=== Inicializando módulo Go ==="
go mod init proxyeuro || echo "go.mod já existe"
go mod tidy || { echo "Erro ao rodar go mod tidy"; exit 1; }

echo "=== Compilando proxy_worker ==="
if go build -o /usr/local/bin/proxy_worker proxy-worker.go; then
    echo "proxy_worker compilado com sucesso"
else
    echo "Erro ao compilar proxy_worker.go"
    exit 1
fi

echo "=== Compilando proxy_manager ==="
if go build -o /usr/local/bin/proxyeuro proxy-manager.go; then
    echo "proxy_manager compilado com sucesso"
else
    echo "Erro ao compilar proxy-manager.go"
    exit 1
fi

echo "=== Limpando cache DNS ==="
systemd-resolve --flush-caches || resolvectl flush-caches || echo "Não foi possível limpar o cache DNS"

echo "=== Instalação concluída com sucesso ==="
echo "Use o comando 'proxyeuro' para abrir o menu"