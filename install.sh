#!/bin/bash

# Instalador completo para ProxyEuro
# Versão: 2.0
# Autor: Jean Fraga
# Repositório: https://github.com/jeanfraga33/proxy-go2

# Verificar execução como root
if [ "$(id -u)" != "0" ]; then
    echo "Este script deve ser executado como root!" >&2
    exit 1
fi

# Verificar sistema operacional
check_os() {
    local supported=0
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        case "$ID" in
            ubuntu)
                [[ "$VERSION_ID" == @(18.04|20.04|22.04|24.04) ]] && supported=1 ;;
            debian)
                [[ "$VERSION_ID" == @(8|9|10|11|12) ]] && supported=1 ;;
        esac
    fi
    
    if [ $supported -eq 0 ]; then
        echo "Sistema não suportado! Use Ubuntu 18/20/22/24 ou Debian 8/9/10/11/12"
        exit 1
    fi
}

# Remover instalações anteriores
clean_previous() {
    echo "Removendo instalações anteriores..."
    systemctl stop proxyeuro@* 2>/dev/null
    systemctl disable proxyeuro@* 2>/dev/null
    rm -rf /usr/local/bin/proxyeuro \
           /etc/proxyeuro \
           /etc/systemd/system/proxyeuro@.service
    systemctl daemon-reload
}

# Instalar dependências
install_deps() {
    echo "Atualizando repositórios..."
    apt-get update -qq
    
    echo "Instalando dependências..."
    apt-get install -y -qq golang git openssl sed
}

# Compilar aplicação
compile_app() {
    local temp_dir=$(mktemp -d)
    
    echo "Baixando código fonte..."
    git clone -q https://github.com/jeanfraga33/proxy-go2.git "$temp_dir"
    cd "$temp_dir"
    
    echo "Aplicando correções..."
    sed -i '
    s/"io"/\/\/ "io"/;
    s/n, err := conn.Read(buffer)/_, err := conn.Read(buffer)/;
    s/porta := os.Args\[1\]/porta, _ := strconv.Atoi(os.Args[1])/;
    s/":"+porta/":"+strconv.Itoa(porta)/;
    /^import (/a \t"strconv"
    ' proxy-manager.go
    
    echo "Compilando binário..."
    go build -o proxyeuro proxy-manager.go
    
    if [ ! -f "proxyeuro" ]; then
        echo "Falha na compilação! Verifique as dependências."
        exit 1
    fi
}

# Configurar sistema
setup_system() {
    echo "Instalando binário..."
    mv proxyeuro /usr/local/bin/
    chmod +x /usr/local/bin/proxyeuro
    
    echo "Criando diretórios..."
    mkdir -p /etc/proxyeuro
    
    echo "Configurando serviço systemd..."
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

# Finalização
cleanup() {
    echo "Limpando cache DNS..."
    resolvectl flush-caches 2>/dev/null || systemctl restart systemd-resolved 2>/dev/null
    
    echo "Removendo arquivos temporários..."
    rm -rf "$temp_dir"
}

# Fluxo principal
main() {
    check_os
    clean_previous
    install_deps
    compile_app
    setup_system
    cleanup
    
    echo -e "\nInstalação concluída com sucesso!"
    echo "Use: proxyeuro <porta> para iniciar o proxy"
    echo "Exemplo: proxyeuro 8080"
}

# Executar instalação
main
