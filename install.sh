#!/bin/bash

# Script de instalação do Proxy Euro com dependências e limpeza de instalação anterior

set -e

# Verifica se executado como root
if [ "$(id -u)" -ne 0 ]; then
  echo "Por favor, execute este script como root: sudo $0"
  exit 1
fi

INSTALL_DIR="/usr/local/bin"
BIN_NAME="proxyeuro"  # ajuste conforme binário gerado
TMP_DIR=$(mktemp -d)
REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
GO_VERSION="1.20"

handle_error() {
  echo -e "\n\e[31m❌ Erro crítico: $1\e[0m"
  rm -rf "$TMP_DIR"
  exit 1
}

echo "Removendo instalação anterior se existir..."
if [ -f "$INSTALL_DIR/$BIN_NAME" ]; then
  rm -f "$INSTALL_DIR/$BIN_NAME"
  echo "Binário antigo removido."
fi

echo "Instalando dependências essenciais..."
apt-get install -y -qq git openssl wget tar golang nginx iptables || handle_error "Falha ao instalar dependências"

echo "Clonando repositório..."
git clone -q "$REPO_URL" "$TMP_DIR"

cd "$TMP_DIR"

echo "Compilando o proxy..."
go mod init proxyeuro 2>/dev/null || true
go build -o "$BIN_NAME" proxy-manager.go || handle_error "Falha ao compilar o proxy"

echo "Copiando o binário para $INSTALL_DIR..."
cp "$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
chmod +x "$INSTALL_DIR/$BIN_NAME"

echo "Limpando arquivos temporários..."
cd /
rm -rf "$TMP_DIR"

echo -e "\n✅ Proxy Euro instalado com sucesso!"
echo "Para iniciar o proxy, execute o comando (substitua <porta> pela porta desejada):"
echo -e "  sudo $INSTALL_DIR/$BIN_NAME <porta>"
echo "O proxy abrirá automaticamente a porta no firewall e configurará o Nginx."