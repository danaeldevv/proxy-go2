#!/bin/bash

# Instalador Oficial ProxyEuro - Versão 6.0
# Com todas as correções de compilação aplicadas
# Repositório: https://github.com/jeanfraga33/proxy-go2

set -euo pipefail

# Verificar root
[ "$(id -u)" != "0" ] && echo "Execute como root: sudo $0" && exit 1

# Configurações
REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"

# Verificar sistema
check_os() {
    if ! grep -qEi "(ubuntu|debian)" /etc/os-release; then
        echo "Sistema não suportado! Use Ubuntu/Debian."
        exit 1
    fi
}

# Aplicar patches no código
apply_patches() {
    local src_file="$1/proxy-manager.go"
    
    echo "Aplicando correções no código fonte..."
    
    # Correção de imports e variáveis
    sed -i '
    /^import (/ {
        s/"io"/\/\/ "io"/;
        s/"strconv"/"strconv"/;
    }
    s/n, err := conn.Read(buffer)/_, err := conn.Read(buffer)/;
    s/porta := os.Args\[1\]/porta, _ := strconv.Atoi(os.Args[1])/;
    s/":"+porta/":"+strconv.Itoa(porta)/g;
    s/log.Printf("Erro ao ler dados iniciais: %v", err)/log.Printf("Erro na leitura: %v", err)/;
    ' "$src_file"
}

# Instalar dependências
install_deps() {
    echo "Instalando dependências..."
    apt-get update -qq
    apt-get install -y -qq golang git openssl
}

# Compilar aplicação
compile_app() {
    local temp_dir=$(mktemp -d)
    
    echo "Baixando código fonte..."
    git clone -q "$REPO_URL" "$temp_dir"
    
    apply_patches "$temp_dir"
    
    echo "Compilando aplicação..."
    cd "$temp_dir"
    go build -o proxyeuro proxy-manager.go
    
    [ ! -f "proxyeuro" ] && echo "Erro na compilação!" && exit 1
}

# Configurar sistema
setup_system() {
    echo "Configurando serviços..."
    
    # Instalar binário
    install -m 755 proxyeuro "$INSTALL_DIR"
    
    # Criar serviço systemd
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

# Main
main() {
    check_os
    install_deps
    compile_app
    setup_system
    
    echo -e "\n✅ Instalação concluída com sucesso!"
    echo "Como usar:"
    echo "Abrir porta:  proxyeuro <porta>"
    echo "Exemplo:     proxyeuro 8080"
    echo "Ver status:  systemctl status proxyeuro@8080"
}

main
