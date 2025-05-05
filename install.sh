#!/bin/bash

# Verifica se o script está sendo executado como root
if [ "$(id -u)" -ne 0 ]; then
    echo "Execute como root: sudo $0"
    exit 1
fi

# Configurações
INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"
LOG_FILE="/var/log/proxyws.log"
PID_DIR="/var/run"
REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
TMP_DIR=$(mktemp -d)

# Função para tratamento de erros
handle_error() {
    echo "❌ Erro crítico: $1"
    rm -rf "$TMP_DIR"
    exit 1
}

# Limpar cache DNS
clear_dns_cache() {
    echo "Limpando tabela de cache DNS..."
    systemd-resolve --flush-caches || echo "❌ Falha ao limpar cache DNS."
}

# Remover instalação anterior
cleanup() {
    echo "Verificando instalação anterior..."
    if [ -f "$INSTALL_DIR/proxyeuro" ]; then
        echo "Removendo instalação anterior..."
        systemctl stop proxyeuro@* 2>/dev/null
        systemctl disable proxyeuro@* 2>/dev/null
        rm -f "$INSTALL_DIR/proxyeuro" "$SERVICE_DIR/proxyeuro@.service"
        systemctl daemon-reload
    fi
}

# Instalar dependências
install_deps() {
    echo "Instalando dependências..."
    apt-get update -qq || handle_error "Falha ao atualizar pacotes"
    apt-get install -y -qq golang git openssl || handle_error "Falha ao instalar dependências"
}

# Gerar certificados
generate_certificates() {
    echo "Gerando certificados SSL..."
    mkdir -p /etc/ssl/proxyeuro
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout /etc/ssl/proxyeuro/key.pem \
        -out /etc/ssl/proxyeuro/cert.pem \
        -subj "/C=BR/ST=State/L=City/O=Organization/OU=Unit/CN=example.com" || handle_error "Falha ao gerar certificados"
}

# Clonar repositório e preparar o ambiente
prepare_environment() {
    echo "Clonando repositório..."
    git clone -q "$REPO_URL" "$TMP_DIR" || handle_error "Falha ao clonar repositório"
    cd "$TMP_DIR" || handle_error "Falha ao acessar diretório temporário"
}

# Instalar o proxy
install_proxy() {
    echo "Instalando o proxy..."
    cp proxy-manager.go "$INSTALL_DIR/proxyeuro" || handle_error "Falha ao copiar o arquivo do proxy"
    chmod +x "$INSTALL_DIR/proxyeuro"
    
    echo "Configurando serviço..."
    cat > "$SERVICE_DIR/proxyeuro@.service" <<EOF || handle_error "Falha ao criar serviço"
[Unit]
Description=ProxyEuro na porta %i
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/proxyeuro %i
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
}

# Fluxo principal
main() {
    clear_dns_cache
    cleanup
    install_deps
    generate_certificates
    prepare_environment
    install_proxy
    
    echo -e "\n✅ Instalação concluída com sucesso!"
    echo "Para abrir o menu do proxy, use o comando:"
    echo "proxyeuro"
}

# Executar instalação
main
rm -rf "$TMP_DIR"
