#!/bin/bash

# Instalador Oficial ProxyEuro
# Versão: 1.0
# Autor: Jean Fraga
# Repositório: https://github.com/jeanfraga33/proxy-go2

# Verificar root
if [ "$(id -u)" != "0" ]; then
    echo "Este script deve ser executado como root!" >&2
    exit 1
fi

# Configurações
INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"
CERT_DIR="/etc/proxyeuro"
REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"

# Função para limpar instalação anterior
clean_previous() {
    echo "Removendo instalações anteriores..."
    
    # Parar e desabilitar serviços
    systemctl stop proxyeuro@* 2>/dev/null
    systemctl disable proxyeuro@* 2>/dev/null
    
    # Remover arquivos
    rm -f "$INSTALL_DIR/proxyeuro"
    rm -f "$SERVICE_DIR/proxyeuro@.service"
    rm -rf "$CERT_DIR"
    
    # Recarregar systemd
    systemctl daemon-reload
}

# Instalar dependências
install_dependencies() {
    echo "Instalando dependências..."
    apt-get update -qq
    apt-get install -y -qq \
        golang \
        git \
        openssl \
        sed \
        systemd
}

# Compilar e instalar
compile_install() {
    local temp_dir=$(mktemp -d)
    
    echo "Baixando código fonte..."
    git clone -q "$REPO_URL" "$temp_dir"
    cd "$temp_dir"

    echo "Compilando aplicação..."
    go build -o proxyeuro proxy-manager.go
    
    if [ ! -f "proxyeuro" ]; then
        echo "Erro na compilação!" >&2
        exit 1
    fi

    echo "Instalando binário..."
    install -m 755 proxyeuro "$INSTALL_DIR"
}

# Configurar systemd
setup_systemd() {
    echo "Configurando serviços systemd..."
    
    cat > "$SERVICE_DIR/proxyeuro@.service" <<EOF
[Unit]
Description=ProxyEuro na porta %I
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/proxyeuro %i
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
}

# Configurar ambiente
setup_environment() {
    echo "Criando diretório de certificados..."
    mkdir -p "$CERT_DIR"
    chmod 700 "$CERT_DIR"
}

# Main
main() {
    # Etapas de instalação
    clean_previous
    install_dependencies
    compile_install
    setup_systemd
    setup_environment
    
    # Resultado final
    echo -e "\n✅ Instalação concluída com sucesso!"
    echo -e "\nComo usar:"
    echo "1. Abrir porta:  proxyeuro <porta>"
    echo "2. Ver status:   systemctl status proxyeuro@<porta>"
    echo "3. Fechar porta: systemctl stop proxyeuro@<porta>"
    echo -e "\nExemplo:"
    echo "proxyeuro 8080"
    echo "systemctl status proxyeuro@8080"
}

# Executar instalação
main
