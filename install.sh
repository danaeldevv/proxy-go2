#!/bin/bash

set -e

# Função para tratamento de erros
handle_error() {
    echo "Erro durante a instalação. Verifique as mensagens acima."
    exit 1
}

trap handle_error ERR

# Verificar se é root
if [ "$(id -u)" -ne 0 ]; then
    echo "Execute o script como root!"
    exit 1
fi

# Configurações
REPO_URL="https://raw.githubusercontent.com/jeanfraga33/proxy-go2/main/proxy-manager.go"
INSTALL_DIR="/usr/local/bin"
CERT_DIR="/etc/multiprocy"

# Limpar instalações anteriores
echo "Limpando instalações anteriores..."

# Parar e remover serviços systemd
for service in $(systemctl list-unit-files --no-legend | grep -o "^multiproxy-port-[0-9]\+.service"); do
    echo "Removendo serviço $service"
    systemctl stop "$service" 2>/dev/null || true
    systemctl disable "$service" 2>/dev/null || true
    rm -f "/etc/systemd/system/$service" || true
done

# Recarregar systemd
systemctl daemon-reload 2>/dev/null || true

# Remover binário antigo
rm -f "$INSTALL_DIR/multiproxy"

# Instalar dependências
echo "Instalando dependências..."
 apt-get update -y
if ! command -v git >/dev/null; then
    apt-get install -y git
fi

if ! command -v go >/dev/null; then
    apt-get install -y golang
fi

if ! command -v openssl >/dev/null; then
    apt-get install -y openssl
fi

# Criar diretório para certificados
mkdir -p "$CERT_DIR"

# Gerar certificados SSL se não existirem
if [ ! -f "$CERT_DIR/server.crt" ] || [ ! -f "$CERT_DIR/server.key" ]; then
    echo "Gerando certificados TLS..."
    openssl req -x509 -newkey rsa:4096 -nodes -days 365 \
        -keyout "$CERT_DIR/server.key" \
        -out "$CERT_DIR/server.crt" \
        -subj "/CN=localhost" 2>/dev/null
fi

# Baixar e compilar
TMP_DIR=$(mktemp -d)
echo "Baixando código do GitHub..."
if ! curl -sSL -o "$TMP_DIR/proxy-manager.go" "$REPO_URL"; then
    echo "Falha no download do código fonte!"
    exit 1
fi

# Validar arquivo baixado
if [ ! -s "$TMP_DIR/proxy-manager.go" ] || ! grep -q '^package main' "$TMP_DIR/proxy-manager.go"; then
    echo "Arquivo baixado inválido ou corrompido!"
    exit 1
fi

# Ajustar caminho dos certificados no código
echo "Ajustando configurações..."
sed -i "s|certFile      = \"server.crt\"|certFile      = \"$CERT_DIR/server.crt\"|g" "$TMP_DIR/proxy-manager.go"
sed -i "s|keyFile       = \"server.key\"|keyFile       = \"$CERT_DIR/server.key\"|g" "$TMP_DIR/proxy-manager.go"

# Criar módulo Go temporário
echo "Criando módulo Go..."
(cd "$TMP_DIR" && \
    go mod init proxy-manager && \
    go get golang.org/x/net/websocket && \
    go mod tidy)

# Compilar
echo "Compilando..."
(cd "$TMP_DIR" && go build -o multiproxy .)

# Instalar
echo "Instalando binário em $INSTALL_DIR..."
mv "$TMP_DIR/multiproxy" "$INSTALL_DIR/"
chmod +x "$INSTALL_DIR/multiproxy"

# Limpar
rm -rf "$TMP_DIR"

echo "
Instalação concluída com sucesso!
Comandos disponíveis:
- Iniciar o proxy: multiproxy
- Gerenciar portas: multiproxy (via menu interativo)
- Certificados TLS em: $CERT_DIR
"
