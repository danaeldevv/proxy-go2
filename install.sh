#!/bin/bash

# Script simples para instalar o binário do Proxy Euro e exibir o comando para uso

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

echo "Clonando repositório..."
git clone -q "$REPO_URL" "$TMP_DIR"

cd "$TMP_DIR"

echo "Compilando o proxy..."
go mod init proxyeuro 2>/dev/null || true
go build -o "$BIN_NAME" proxy-manager.go

echo "Copiando o binário para $INSTALL_DIR..."
cp "$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
chmod +x "$INSTALL_DIR/$BIN_NAME"

echo "Limpando..."
cd /
rm -rf "$TMP_DIR"

echo -e "\n✅ Proxy Euro instalado com sucesso!"
echo "Para iniciar o proxy execute o comando, substituindo <porta> pela porta desejada:"
echo -e "  sudo $INSTALL_DIR/$BIN_NAME <porta>"
echo "Exemplo:"
echo -e "  sudo $INSTALL_DIR/$BIN_NAME 1080"