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
GO_VERSION="1.20"  # Defina a versão mínima do Go necessária

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
    apt-get install -y -qq git openssl wget tar || handle_error "Falha ao instalar dependências"
}

# Instalar Go
install_go() {
    echo "Verificando a instalação do Go..."
    if ! command -v go &> /dev/null; then
        echo "Go não encontrado. Instalando a versão mais recente..."
        wget https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz -O /tmp/go.tar.gz || handle_error "Falha ao baixar Go"
        rm -rf /usr/local/go
        tar -C /usr/local -xzf /tmp/go.tar.gz || handle_error "Falha ao instalar Go"
        echo "export PATH=\$PATH:/usr/local/go/bin" >> /etc/profile.d/go.sh
        chmod +x /etc/profile.d/go.sh
        source /etc/profile.d/go.sh
    else
        INSTALLED_GO_VERSION=$(go version | awk '{print $3}' | cut -d. -f2)
        MIN_GO_VERSION=${GO_VERSION%.*}  # Exemplo: "1.20" -> "1"
        if [ "$INSTALLED_GO_VERSION" -lt "$MIN_GO_VERSION" ]; then
            echo "A versão do Go instalada é inferior à versão mínima necessária ($GO_VERSION). Atualizando..."
            wget https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz -O /tmp/go.tar.gz || handle_error "Falha ao baixar Go"
            rm -rf /usr/local/go
            tar -C /usr/local -xzf /tmp/go.tar.gz || handle_error "Falha ao instalar Go"
            echo "export PATH=\$PATH:/usr/local/go/bin" >> /etc/profile.d/go.sh
            chmod +x /etc/profile.d/go.sh
            source /etc/profile.d/go.sh
        else
            echo "Go já está instalado e atende aos requisitos."
        fi
    fi
    # Confirma a versão do Go após instalação/atualização
    go version
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

# Compilar o proxy
compile_proxy() {
    echo "Compilando o proxy..."
    go mod init proxyeuro 2>/dev/null || true
    go build -o proxyeuro proxy-manager.go || handle_error "Falha ao compilar o proxy"
}

# Instalar o binário do proxy
install_proxy() {
    echo "Instalando o proxy ..."
    cp proxyeuro "$INSTALL_DIR/proxyeuro" || handle_error "Falha ao copiar o binário"
    chmod +x "$INSTALL_DIR/proxyeuro"
    
    echo "Configurando serviço systemd..."
    cat > "$SERVICE_DIR/proxyeuro@.service" <<EOF || handle_error "Falha ao criar serviço"
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

# Fluxo principal
main() {
    clear_dns_cache
    cleanup
    install_deps
    install_go
    generate_certificates
    prepare_environment
    compile_proxy
    install_proxy
    
    echo -e "\n✅ Instalação concluída com sucesso!"
    echo "Para abrir o menu do proxy, use o comando:"
    echo "proxyeuro"
}

# Executar instalação
main
rm -rf "$TMP_DIR"
