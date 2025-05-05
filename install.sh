#!/bin/bash

# Verificar se é root
if [ "$(id -u)" != "0" ]; then
    echo "Este script deve ser executado como root" 1>&2
    exit 1
fi

# Verificar sistema compatível
supported=false
if [ -f /etc/os-release ]; then
    . /etc/os-release
    case "$ID" in
        ubuntu)
            case "$VERSION_ID" in
                18.04|20.04|22.04|24.04) supported=true ;;
            esac
            ;;
        debian)
            case "$VERSION_ID" in
                8|9|10|11|12) supported=true ;;
            esac
            ;;
    esac
fi

if [ "$supported" != "true" ]; then
    echo "Sistema não suportado"
    exit 1
fi

# Remover instalação anterior
echo "Removendo instalações anteriores..."
systemctl stop proxyeuro@* >/dev/null 2>&1
systemctl disable proxyeuro@* >/dev/null 2>&1
rm -rf /usr/local/bin/proxyeuro
rm -rf /usr/local/bin/proxy_worker
rm -f /etc/systemd/system/proxyeuro@.service
rm -rf /etc/proxyeuro
systemctl daemon-reload

# Instalar dependências
echo "Instalando dependências..."
apt-get install -y golang git openssl

# Baixar e construir o aplicativo
echo "Baixando e construindo o proxy..."
temp_dir=$(mktemp -d)
cd "$temp_dir"

git clone https://github.com/jeanfraga33/proxy-go2.git
cd proxy-go2

# Corrigir nome do arquivo se necessário
if [ -f "proxy-manager.go" ]; then
    mv "proxy-manager.go" "proxy_manager.go"
fi

go build -o proxyeuro proxy_manager.go

# Instalar binários
echo "Instalando binários..."
mv proxyeuro /usr/local/bin/
chmod +x /usr/local/bin/proxyeuro

# Criar diretório de certificados
mkdir -p /etc/proxyeuro

# Criar arquivo de serviço
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

# Limpar cache DNS
echo "Limpando cache DNS..."
resolvectl flush-caches >/dev/null 2>&1 || systemctl restart systemd-resolved >/dev/null 2>&1

# Limpar arquivos temporários
echo "Limpando arquivos temporários..."
rm -rf "$temp_dir"

echo "Instalação concluída!"
echo "Use o comando 'proxyeuro' para iniciar o gerenciador de proxy"
