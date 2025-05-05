#!/bin/bash

# Instalador ProxyEuro
# Versão: 3.0
# Repositório: https://github.com/seu-usuario/proxy-go2

if [ "$(id -u)" != "0" ]; then
    echo "Execute como root: sudo $0"
    exit 1
fi

echo "=== Instalação ProxyEuro ==="

# Remover versões anteriores
systemctl stop proxyeuro@* 2>/dev/null
systemctl disable proxyeuro@* 2>/dev/null
rm -rf /usr/local/bin/proxyeuro 
rm -f /etc/systemd/system/proxyeuro@.service

# Instalar dependências
apt-get update
apt-get install -y golang git openssl

# Compilar e instalar
TMP_DIR=$(mktemp -d)
git clone https://github.com/seu-usuario/proxy-go2.git $TMP_DIR
cd $TMP_DIR
go build -o proxyeuro
install -m 755 proxyeuro /usr/local/bin/

# Configurar systemd
cat > /etc/systemd/system/proxyeuro@.service <<EOF
[Unit]
Description=ProxyEuro na porta %I
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/proxyeuro --service %i
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload

echo -e "\nInstalação concluída!\n"
echo "Como usar:"
echo "Abrir porta:  proxyeuro <porta>"
echo "Exemplo:     proxyeuro 80"
echo "Ver status:  systemctl status proxyeuro@80"
