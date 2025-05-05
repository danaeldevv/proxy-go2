#!/bin/bash

# Instalador Oficial ProxyEuro - Versão 5.0
# Repositório: https://github.com/jeanfraga33/proxy-go2

# Verificar root
if [ "$(id -u)" != "0" ]; then
    echo "Execute como root: sudo $0"
    exit 1
fi

# Configurações
REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
TMP_DIR=$(mktemp -d)
INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"

# Função para tratamento de erros
handle_error() {
    echo "❌ Erro crítico: $1"
    rm -rf "$TMP_DIR"
    exit 1
}

# Remover instalações anteriores
cleanup() {
    echo "Removendo instalações anteriores..."
    systemctl stop proxyeuro@* 2>/dev/null
    systemctl disable proxyeuro@* 2>/dev/null
    rm -rf "$INSTALL_DIR/proxyeuro" "$SERVICE_DIR/proxyeuro@.service"
    systemctl daemon-reload
}

# Instalar dependências
install_deps() {
    echo "Instalando dependências..."
    apt-get update -qq || handle_error "Falha ao atualizar pacotes"
    apt-get install -y -qq golang git openssl || handle_error "Falha ao instalar dependências"
}

# Compilar aplicação
compile_app() {
    echo "Clonando repositório..."
    git clone -q "$REPO_URL" "$TMP_DIR" || handle_error "Falha ao clonar repositório"
    
    cd "$TMP_DIR" || handle_error "Falha ao acessar diretório temporário"
    
    echo "Inicializando módulo Go..."
    go mod init proxyeuro || handle_error "Falha ao inicializar módulo Go"
    
    echo "Compilando aplicação..."
    go build -o proxyeuro . || handle_error "Falha na compilação"
    
    [ ! -f "proxyeuro" ] && handle_error "Binário não gerado"
}

# Instalar no sistema
install_system() {
    echo "Instalando binário..."
    install -m 755 proxyeuro "$INSTALL_DIR" || handle_error "Falha na instalação"
    
    echo "Configurando serviço..."
    cat > "$SERVICE_DIR/proxyeuro@.service" <<EOF || handle_error "Falha ao criar serviço"
[Unit]
Description=ProxyEuro na porta %I
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/proxyeuro --service %i
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
}

# Fluxo principal
main() {
    cleanup
    install_deps
    compile_app
    install_system
    
    echo -e "\n✅ Instalação concluída com sucesso!"
    echo "Como usar:"
    echo "Abrir porta:  proxyeuro <porta>"
    echo "Exemplo:     proxyeuro 8080"
    echo "Ver status:  systemctl status proxyeuro@8080"
}

# Executar instalação
main
rm -rf "$TMP_DIR"
