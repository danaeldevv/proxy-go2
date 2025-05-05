#!/bin/bash

# Instalador ProxyEuro - Versão 5.0
# Correção definitiva para problemas de execução

set -euo pipefail

# Verificar root
[ "$(id -u)" != "0" ] && echo "Execute como root: sudo $0" && exit 1

# Função de log
log() {
    echo -e "\n[$(date '+%H:%M:%S')] $1"
}

# Verificar sistema
check_os() {
    log "Verificando sistema..."
    if ! grep -qEi "(ubuntu|debian)" /etc/os-release; then
        log "Sistema não suportado! Use Ubuntu/Debian"
        exit 1
    fi
}

# Instalar dependências
install_deps() {
    log "Instalando dependências..."
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq \
        golang \
        git \
        openssl \
        sed \
        ca-certificates
}

# Compilar aplicação
compile_app() {
    local tmp_dir
    tmp_dir=$(mktemp -d)
    
    log "Baixando código fonte..."
    git clone -q https://github.com/jeanfraga33/proxy-go2.git "$tmp_dir"
    cd "$tmp_dir"

    log "Aplicando correções..."
    sed -i '
    s/"io"/\/\/ "io"/;
    s/n, err := conn.Read(buffer)/_, err := conn.Read(buffer)/;
    s/porta := os.Args\[1\]/porta, _ := strconv.Atoi(os.Args[1])/;
    s/":"+porta/":"+strconv.Itoa(porta)/;
    ' proxy-manager.go

    sed -i '/import (/a \t"strconv"' proxy-manager.go

    log "Compilando..."
    export GO111MODULE=auto
    go build -o proxyeuro proxy-manager.go
    [ ! -f "proxyeuro" ] && log "Falha na compilação!" && exit 1
}

# Configurar sistema
setup_system() {
    log "Configurando ambiente..."
    install -m 755 proxyeuro /usr/local/bin/
    mkdir -p /etc/proxyeuro

    cat > /etc/systemd/system/proxyeuro@.service <<EOF
[Unit]
Description=ProxyEuro na porta %I
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/proxyeuro %i
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
    log "✅ Instalação concluída!\n"
    echo "Uso: proxyeuro <porta>"
    echo "Exemplo: proxyeuro 8080"
}

main "$@"
