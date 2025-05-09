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
TMP_DIR=$(mktemp -d)
REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
GO_VERSION="1.20"  # Versão mínima do Go
NGINX_CONF="/etc/nginx/sites-available/proxyeuro"

# Função para tratamento de erros
handle_error() {
    echo -e "\n\e[31m❌ Erro crítico: $1\e[0m"
    rm -rf "$TMP_DIR"
    exit 1
}

# Barra de progresso simples
progress_bar() {
    local duration=$1
    echo -n "["
    for ((i=0; i<duration; i++)); do
        echo -n "#"
        sleep 0.2
    done
    echo "]"
}

# Execute um comando mostrando label e barra de progresso
run_step() {
    local label="$1"
    local command="$2"
    echo -ne "$label..."
    eval "$command"
    if [ $? -ne 0 ]; then
        handle_error "$label falhou!"
    fi
    progress_bar 10
    echo " concluído."
}

# Limpar cache DNS
clear_dns_cache() {
    systemd-resolve --flush-caches || echo "Falha ao limpar cache DNS."
}

# Remover instalação anterior
cleanup() {
    if [ -f "$INSTALL_DIR/proxyeuro" ]; then
        systemctl stop proxyeuro@* 2>/dev/null
        systemctl disable proxyeuro@* 2>/dev/null
        rm -f "$INSTALL_DIR/proxyeuro" "$SERVICE_DIR/proxyeuro@.service"
        systemctl daemon-reload
    fi
}

# Instalar dependências
install_deps() {
    apt-get update -qq || handle_error "Atualizar pacotes falhou"
    apt-get install -y -qq git openssl wget tar nginx || handle_error "Instalar dependências falhou"
}

# Instalar Go
install_go() {
    if ! command -v go &> /dev/null; then
        wget https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz -O /tmp/go.tar.gz || handle_error "Baixar Go falhou"
        rm -rf /usr/local/go
        tar -C /usr/local -xzf /tmp/go.tar.gz || handle_error "Extrair Go falhou"
        echo "export PATH=\$PATH:/usr/local/go/bin" > /etc/profile.d/go.sh
        chmod +x /etc/profile.d/go.sh
        source /etc/profile.d/go.sh
    else
        INSTALLED_GO_VERSION=$(go version | awk '{print $3}' | cut -d. -f2)
        MIN_GO_VERSION=${GO_VERSION%.*}
        if [ "$INSTALLED_GO_VERSION" -lt "$MIN_GO_VERSION" ]; then
            wget https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz -O /tmp/go.tar.gz || handle_error "Atualizar Go falhou"
            rm -rf /usr/local/go
            tar -C /usr/local -xzf /tmp/go.tar.gz || handle_error "Extrair Go falhou"
            echo "export PATH=\$PATH:/usr/local/go/bin" > /etc/profile.d/go.sh
            chmod +x /etc/profile.d/go.sh
            source /etc/profile.d/go.sh
        fi
    fi
}

# Gerar certificados
generate_certificates() {
    mkdir -p /etc/ssl/proxyeuro
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout /etc/ssl/proxyeuro/key.pem \
        -out /etc/ssl/proxyeuro/cert.pem \
        -subj "/C=BR/ST=State/L=City/O=Organization/OU=Unit/CN=example.com" || handle_error "Gerar certificados falhou"
}

# Clonar repositório
prepare_environment() {
    git clone -q "$REPO_URL" "$TMP_DIR" || handle_error "Clonar repositório falhou"
    cd "$TMP_DIR" || handle_error "Acessar diretório temporário falhou"
}

# Compilar proxy
compile_proxy() {
    go mod init proxyeuro 2>/dev/null || true
    go build -o proxyeuro proxy-manager.go || handle_error "Compilar proxy falhou"
}

# Instalar proxy e configurar serviço
install_proxy() {
    cp proxyeuro "$INSTALL_DIR/proxyeuro" || handle_error "Copiar binário falhou"
    chmod +x "$INSTALL_DIR/proxyeuro"

    cat > "$SERVICE_DIR/proxyeuro@.service" <<EOF || handle_error "Criar serviço falhou"
[Unit]
Description=ProxyEuro na porta %i
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/proxyeuro %i
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
}

# Configurar Nginx
configure_nginx() {
    cat > "$NGINX_CONF" <<EOF || handle_error "Criar configuração do Nginx falhou"
server {
    listen 80;
    server_name example.com;

    location / {
        proxy_pass http://localhost:8080;  # Altere a porta conforme necessário
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF

    ln -s "$NGINX_CONF" /etc/nginx/sites-enabled/ || handle_error "Ativar configuração do Nginx falhou"
    systemctl restart nginx || handle_error "Reiniciar Nginx falhou"
}

main() {
    clear_dns_cache
    run_step "Removendo instalação anterior" "cleanup"
    run_step "Instalando dependências" "install_deps"
    run_step "Instalando Go" "install_go"
    run_step "Gerando certificados SSL" "generate_certificates"
    run_step "Preparando ambiente" "prepare_environment"
    run_step "Compilando proxy" "compile_proxy"
    run_step "Instalando proxy e serviço" "install_proxy"
    run_step "Configurando Nginx" "configure_nginx"

    echo -e "\n✅ Instalação concluída!"
    echo "Use o comando para abrir o menu do proxy:"
    echo -e "\e[32mproxyeuro\e[0m"
}

main
rm -rf "$TMP_DIR"