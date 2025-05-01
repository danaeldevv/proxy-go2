#!/bin/bash
set -e

# Instala as dependências
echo "=== Instalando dependências ==="
apt update
apt install -y golang git openssl

# Remover instalação anterior
echo "=== Removendo instalação anterior ==="
rm -rf /opt/proxyeuro

# Clonando o repositório do GitHub
echo "=== Clonando repositório ==="
git clone https://github.com/jeanfraga33/proxy-go2.git /opt/proxyeuro || { echo "Erro ao clonar o repositório"; exit 1; }
cd /opt/proxyeuro

# Gerando certificados TLS autoassinados
echo "=== Gerando certificados TLS ==="
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 700 -nodes -subj "/CN=localhost" || { echo "Erro ao gerar certificados TLS"; exit 1; }

# Inicializando o módulo Go
echo "=== Inicializando módulo Go ==="
go mod init proxyeuro || echo "go.mod já existe"
go mod tidy || { echo "Erro ao rodar go mod tidy"; exit 1; }

# Compilando proxy_worker
echo "=== Compilando proxy_worker ==="
if go build -o /usr/local/bin/proxy_worker proxy-worker.go; then
    echo "proxy_worker compilado com sucesso"
else
    echo "Erro ao compilar proxy_worker.go"
    exit 1
fi

# Compilando proxy_manager
echo "=== Compilando proxy_manager ==="
if go build -o /usr/local/bin/proxyeuro proxy-manager.go; then
    echo "proxy_manager compilado com sucesso"
else
    echo "Erro ao compilar proxy-manager.go"
    exit 1
fi

# Limpando cache DNS
echo "=== Limpando cache DNS ==="
systemd-resolve --flush-caches || resolvectl flush-caches || echo "Não foi possível limpar o cache DNS"

# Finalização da instalação
echo "=== Instalação concluída com sucesso ==="
echo "Use o comando 'proxyeuro' para abrir o menu"