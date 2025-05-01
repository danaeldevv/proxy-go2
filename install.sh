#!/bin/bash
set -e

echo "=== Instalando dependências do sistema ==="
apt update -y && apt install -y golang git openssl || {
    echo "Erro ao instalar dependências."
    exit 1
}

echo "=== Criando diretório do proxy ==="
INSTALL_DIR="/opt/proxyeuro"
mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR" || exit 1

echo "=== Baixando arquivos do proxy ==="
cat > proxy_manager.go << 'EOF'
[COLE AQUI O CÓDIGO COMPLETO DO proxy_manager.go]
EOF

cat > proxy_worker.go << 'EOF'
[COLE AQUI O CÓDIGO COMPLETO DO proxy_worker.go]
EOF

echo "=== Gerando certificados TLS autoassinados ==="
if ! openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365 -nodes -subj "/CN=localhost"; then
    echo "Erro ao gerar certificado TLS."
    exit 1
fi

echo "=== Inicializando módulo Go ==="
if [ ! -f go.mod ]; then
    go mod init proxyeuro || {
        echo "Erro ao inicializar go.mod"
        exit 1
    }
fi

echo "=== Resolvendo dependências ==="
go mod tidy || {
    echo "Erro ao rodar go mod tidy"
    exit 1
}

echo "=== Compilando proxy_worker ==="
if ! go build -o /usr/local/bin/proxy_worker proxy_worker.go; then
    echo "Erro ao compilar proxy_worker.go"
    exit 1
fi

echo "=== Compilando proxy_manager como proxyeuro ==="
if ! go build -o /usr/local/bin/proxyeuro proxy_manager.go; then
    echo "Erro ao compilar proxy_manager.go"
    exit 1
fi

echo "=== Instalação concluída com sucesso! ==="
echo "Use o comando: proxyeuro"