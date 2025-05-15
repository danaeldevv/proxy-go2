#!/bin/bash

# Função para instalar pacotes se não estiverem presentes
install_if_missing() {
    if ! dpkg -l | grep -qw "$1"; then
        echo "Instalando $1..."
        sudo apt-get install -y "$1"
    else
        echo "$1 já está instalado."
    fi
}

# Atualizar lista de pacotes
sudo apt-get update

# Verificar e instalar dependências do sistema
install_if_missing "git"
install_if_missing "python3"
install_if_missing "python3-pip"

# Instalar bibliotecas Python necessárias diretamente
echo "Instalando bibliotecas Python necessárias..."
pip3 install --upgrade pip
pip3 install setuptools wheel

# Se os scripts utilizam alguma biblioteca externa, liste aqui.
# Pelo código anterior, não foram usadas bibliotecas externas específicas.
# Caso futuramente utilize, inclua aqui, ex:
# pip3 install websocket-client PySocks

# Diretório do repositório
REPO_URL="https://github.com/jeanfraga33/proxy-go2.git"
INSTALL_DIR="$HOME/proxy-go2"

# Remover instalação anterior, se existir
if [ -d "$INSTALL_DIR" ]; then
    echo "Removendo instalação anterior..."
    rm -rf "$INSTALL_DIR"
fi

# Clonar o repositório
echo "Clonando o repositório..."
git clone "$REPO_URL" "$INSTALL_DIR"

# Criar links simbólicos para os scripts
echo "Instalando scripts no sistema..."
sudo ln -sf "$INSTALL_DIR/menu.py" /usr/local/bin/proxy-menu
sudo ln -sf "$INSTALL_DIR/proxy_server.py" /usr/local/bin/proxy-server

# Tornar os scripts executáveis
sudo chmod +x /usr/local/bin/proxy-menu
sudo chmod +x /usr/local/bin/proxy-server

echo "Instalação concluída! Você pode executar o menu com o comando 'proxy-menu' e o proxy com 'proxy-server'."
