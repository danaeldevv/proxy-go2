#!/bin/bash
set -e

echo "=== Instalando dependências do sistema ==="
apt update -y && apt install -y golang git

echo "=== Criando diretório /opt/proxyeuro ==="
mkdir -p /opt/proxyeuro
cd /opt/proxyeuro

echo "=== Baixando arquivos do proxy ==="
# Salve os códigos abaixo como arquivos locais
cat > proxy_manager.go << 'EOF'
[CÓDIGO DO proxy_manager.go]
EOF

cat > proxy_worker.go << 'EOF'
[CÓDIGO DO proxy_worker.go]
EOF

echo "=== Gerando certificados TLS autoassinados ==="
openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365 -nodes -subj "/CN=localhost"

echo "=== Inicializando módulo Go ==="
go mod init proxyeuro
go mod tidy

echo "=== Compilando proxy_worker ==="
go build -o /usr/local/bin/proxy_worker proxy_worker.go

echo "=== Compilando proxy_manager como proxyeuro ==="
go build -o /usr/local/bin/proxyeuro proxy_manager.go

echo "=== Instalação concluída com sucesso! ==="
echo "Use o comando: proxyeuro"