#!/bin/bash

# Instalador Oficial ProxyEuro - Versão Final
# Correções aplicadas para Go 1.20+
# Repositório: https://github.com/jeanfraga33/proxy-go2

set -euo pipefail

# Verificar root
[ "$(id -u)" != "0" ] && echo "Execute como root: sudo $0" && exit 1

# Configurar ambiente
export DEBIAN_FRONTEND=noninteractive
TEMPDIR=$(mktemp -d)

# Verificar sistema
check_os() {
    if ! grep -qEi "(ubuntu|debian)" /etc/os-release; then
        echo "Sistema não suportado! Use Ubuntu/Debian"
        exit 1
    fi
}

# Remover instalações antigas
clean_old() {
    systemctl stop proxyeuro@* 2>/dev/null || true
    systemctl disable proxyeuro@* 2>/dev/null || true
    rm -rf /usr/local/bin/proxyeuro \
           /etc/systemd/system/proxyeuro@.service
}

# Instalar dependências
install_deps() {
    apt-get update -qq
    apt-get install -y -qq golang git openssl
}

# Aplicar patches críticos
apply_patches() {
    cd "$TEMPDIR/proxy-go2"
    
    # Correção 1: Remover import não utilizado
    sed -i '/"strconv"/d' proxy-manager.go
    
    # Correção 2: Adicionar import correto
    sed -i '/import (/a \t"strconv"' proxy-manager.go
    
    # Correção 3: Variável 'n' não utilizada
    sed -i 's/n, err := conn.Read(buffer)/_, err := conn.Read(buffer)/' proxy-manager.go
    
    # Correção 4: Conversão de porta
    sed -i 's/porta := os.Args\[1\]/porta, _ := strconv.Atoi(os.Args[1])/' proxy-manager.go
    sed -i 's/":"+porta/":"+strconv.Itoa(porta)/g' proxy-manager.go
}

# Compilar aplicação
compile_app() {
    git clone -q https://github.com/jeanfraga33/proxy-go2.git "$TEMPDIR/proxy-go2"
    apply_patches
    
    echo "Compilando aplicação..."
    cd "$TEMPDIR/proxy-go2"
    go build -o proxyeuro proxy-manager.go
}

# Instalar no sistema
install_system() {
    install -m 755 "$TEMPDIR/proxy-go2/proxyeuro" /usr/local/bin/
    
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

    systemctl daemon-reload
}

main() {
    check_os
    clean_old
    install_deps
    compile_app
    install_system
    
    echo -e "\n✅ Instalação concluída com sucesso!"
    echo "Use: proxyeuro <porta>"
    echo "Exemplo: proxyeuro 8080"
}

main
