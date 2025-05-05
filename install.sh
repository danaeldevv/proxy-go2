#!/bin/bash

# Verificar root
if [ "$(id -u)" != "0" ]; then
    echo "Execute como root: sudo ./install_proxyeuro.sh"
    exit 1
fi

# Verificar sistema
OS_ID=$(grep -oP '(?<=^ID=).+' /etc/os-release | tr -d '"')
OS_VERSION=$(grep -oP '(?<=^VERSION_ID=).+' /etc/os-release | tr -d '"')

VALID_OS=false
case "$OS_ID" in
    ubuntu)
        [[ "$OS_VERSION" =~ ^(18.04|20.04|22.04|24.04)$ ]] && VALID_OS=true
        ;;
    debian)
        [[ "$OS_VERSION" =~ ^(8|9|10|11|12)$ ]] && VALID_OS=true
        ;;
esac

if ! $VALID_OS; then
    echo "Sistema não suportado!"
    exit 1
fi

# Limpeza anterior
echo "Removendo instalações anteriores..."
systemctl stop proxyeuro@* 2>/dev/null
systemctl disable proxyeuro@* 2>/dev/null
rm -rf /usr/local/bin/proxyeuro /usr/local/bin/proxy_worker
rm -f /etc/systemd/system/proxyeuro@.service
systemctl daemon-reload

# Instalar dependências
echo "Instalando dependências..."
apt-get install -y golang git openssl

# Clonar e compilar
TMP_DIR=$(mktemp -d)
cd "$TMP_DIR"

echo "Clonando repositório..."
git clone https://github.com/jeanfraga33/proxy-go2.git
cd proxy-go2

# Corrigir nome do arquivo se necessário
[[ -f "proxy-manager.go" ]] && mv "proxy-manager.go" "proxyeuro.go"

echo "Compilando aplicação..."
go build -o proxyeuro proxyeuro.go

if [ ! -f "proxyeuro" ]; then
    echo "Erro na compilação! Verifique as dependências Go."
    exit 1
fi

# Instalar
echo "Instalando binários..."
mv proxyeuro /usr/local/bin/
chmod +x /usr/local/bin/proxyeuro

# Configurar systemd
echo "Configurando serviços..."
cat > /etc/systemd/system/proxyeuro@.service <<EOF
[Unit]
Description=ProxyEuro na porta %I
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/proxyeuro %i
Restart=always

[Install]
WantedBy=multi-user.target
EOF

# Recarregar systemd
systemctl daemon-reload

# Limpar
echo "Limpando cache..."
rm -rf "$TMP_DIR"
resolvectl flush-caches 2>/dev/null || systemctl restart systemd-resolved 2>/dev/null

echo "Instalação concluída!"
echo "Para usar: proxyeuro <porta>"
