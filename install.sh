#!/bin/bash

# Instalador Oficial ProxyEuro - Versão 3.0
# Corrigido para Go 1.20+ e Systemd
# Suporte: Ubuntu 18/20/22/24 e Debian 9/10/11/12

set -e

# Verificar root
[ "$(id -u)" != "0" ] && echo "Execute como root!" && exit 1

# Verificar sistema
check_os() {
    if ! command -v lsb_release &> /dev/null; then
        apt-get install -y lsb-release
    fi
    
    local os=$(lsb_release -is)
    local ver=$(lsb_release -rs)
    
    case "$os" in
        Ubuntu)
            [[ "$ver" =~ ^(18.04|20.04|22.04|24.04)$ ]] || { echo "Ubuntu $ver não suportado"; exit 1; } ;;
        Debian)
            [[ "$ver" =~ ^(9|10|11|12)$ ]] || { echo "Debian $ver não suportado"; exit 1; } ;;
        *) 
            echo "Sistema não reconhecido"; exit 1 ;;
    esac
}

# Remover instalações antigas
cleanup_old() {
    echo "Removendo instalações anteriores..."
    systemctl stop proxyeuro@* 2>/dev/null || true
    systemctl disable proxyeuro@* 2>/dev/null || true
    rm -rf /usr/local/bin/proxyeuro \
           /etc/systemd/system/proxyeuro@.service \
           /etc/proxyeuro
    systemctl daemon-reload
}

# Instalar dependências
install_deps() {
    echo "Instalando dependências..."
    apt-get update -qq
    apt-get install -y -qq golang-1.20 git openssl sed
}

# Compilar aplicação
compile_app() {
    local tmp_dir=$(mktemp -d)
    
    echo "Baixando e corrigindo código fonte..."
    git clone -q https://github.com/jeanfraga33/proxy-go2.git "$tmp_dir"
    cd "$tmp_dir"

    # Aplicar patches críticos
    sed -i '
    s/"io"/\/\/ "io"/;
    s/n, err := conn.Read(buffer)/_, err := conn.Read(buffer)/;
    s/porta := os.Args\[1\]/porta, _ := strconv.Atoi(os.Args[1])/;
    s/":"+porta/":"+strconv.Itoa(porta)/;
    s/log.Printf("Erro ao ler dados iniciais: %v", err)/log.Printf("Erro de leitura: %v", err)/;
    ' proxy-manager.go

    # Corrigir imports
    sed -i '/import (/a \
    "strconv"' proxy-manager.go

    echo "Compilando versão 3.0..."
    go build -o proxyeuro proxy-manager.go
    
    [ ! -f "proxyeuro" ] && echo "Falha na compilação!" && exit 1
}

# Configurar sistema
setup_system() {
    echo "Configurando ambiente..."
    mv proxyeuro /usr/local/bin/
    chmod 755 /usr/local/bin/proxyeuro
    
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

main() {
    check_os
    cleanup_old
    install_deps
    compile_app
    setup_system
    
    echo -e "\n✅ Instalação concluída!\n"
    echo "Como usar:"
    echo "Abrir porta:  proxyeuro 80"
    echo "Ver status:   systemctl status proxyeuro@80"
    echo "Fechar porta: systemctl stop proxyeuro@80"
}

main
