#!/bin/bash

# Instalador Oficial ProxyEuro - Versão 4.0
# Corrigido para instalação em todas versões do Ubuntu/Debian

set -e

# Verificar root
[ "$(id -u)" != "0" ] && echo "Execute como root!" && exit 1

# Configurar repositórios
setup_repos() {
    echo "Configurando repositórios..."
    if grep -q 'Ubuntu' /etc/os-release; then
        add-apt-repository -y ppa:longsleep/golang-backports
    elif grep -q 'Debian' /etc/os-release; then
        echo "deb http://ftp.debian.org/debian buster-backports main" > /etc/apt/sources.list.d/backports.list
        apt-key adv --keyserver keyserver.ubuntu.com --recv-keys 648ACFD622F3D138
    fi
    apt-get update -qq
}

# Instalar dependências
install_deps() {
    echo "Instalando dependências..."
    apt-get install -y -qq \
        golang \
        git \
        openssl \
        sed \
        software-properties-common
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

    # Adicionar import faltante
    if ! grep -q 'strconv' proxy-manager.go; then
        sed -i '/import (/a \t"strconv"' proxy-manager.go
    fi

    echo "Compilando..."
    export GO111MODULE=auto
    go build -o proxyeuro proxy-manager.go
    
    [ ! -f "proxyeuro" ] && echo "Falha na compilação!" && exit 1
}

# Restante do script mantido igual (setup_system, main, etc)
# ... (manter as outras funções do script anterior)

main() {
    check_os
    setup_repos
    cleanup_old
    install_deps
    compile_app
    setup_system
    
    echo -e "\n✅ Instalação concluída!\n"
    echo "Versão Go instalada: $(go version)"
    echo "Como usar:"
    echo "Abrir porta:  proxyeuro 80"
    echo "Ver status:   systemctl status proxyeuro@80"
}
