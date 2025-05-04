#!/bin/bash
set -e

echo "=== Instalando dependências ==="
apt update
apt install -y golang git openssl

echo "=== Removendo instalação anterior (se houver) ==="
rm -rf /opt/proxyeuro
rm -f /usr/local/bin/proxyeuro /usr/local/bin/proxy_worker

echo "=== Clonando repositório ==="
git clone https://github.com/jeanfraga33/proxy-go2.git /opt/proxyeuro

echo "=== Gerando certificados TLS ==="
mkdir -p /opt/proxyeuro/certs
cd /opt/proxyeuro
openssl req -x509 -newkey rsa:2048 -keyout certs/key.pem -out certs/cert.pem -days 700 -nodes -subj "/CN=localhost"

echo "=== Inicializando módulo Go ==="
cd /opt/proxyeuro
go mod init proxyeuro || echo "go.mod já existe"
go mod tidy

echo "=== Compilando proxy_worker ==="
if go build -o /usr/local/bin/proxy_worker proxy-worker.go; then
    echo "✅ proxy_worker compilado com sucesso"
else
    echo "❌ Erro ao compilar proxy_worker.go"
    exit 1
fi

echo "=== Compilando proxy_manager ==="
if go build -o /usr/local/bin/proxyeuro proxy-manager.go; then
    echo "✅ proxy_manager compilado com sucesso"
else
    echo "❌ Erro ao compilar proxy-manager.go"
    exit 1
fi

echo "=== Criando alias global 'proxyeuro' (se necessário) ==="
grep -qxF 'alias proxyeuro="/usr/local/bin/proxyeuro"' /etc/bash.bashrc || echo 'alias proxyeuro="/usr/local/bin/proxyeuro"' >> /etc/bash.bashrc

echo "=== Recarregando systemd (por segurança) ==="
systemctl daemon-reexec
systemctl daemon-reload

echo "✅ Instalação concluída com sucesso!"
echo "Use o comando 'proxyeuro' para abrir o menu"
