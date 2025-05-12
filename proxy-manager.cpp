#include <boost/asio.hpp>
#include <boost/asio/ssl.hpp>
#include <iostream>
#include <fstream>
#include <thread>
#include <unistd.h>
#include <csignal>
#include <cstring>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <vector>
#include <mutex>
#include <map>
#include <chrono>
#include <atomic>
#include <openssl/ssl.h>
#include <openssl/err.h>
#include <openssl/bio.h>
#include <openssl/evp.h>
#include <sys/wait.h>
#include <sys/types.h>
#include <dirent.h>

namespace asio = boost::asio;

// Constants
#define BUFFER_SIZE 8192

// Globals
std::atomic<bool> running(true);
const std::string log_file_path = "/var/log/proxyws.log";

std::mutex log_mutex;
SSL_CTX *ssl_ctx;

// Map port to pid of proxy process
std::map<int, pid_t> proxy_processes;
std::mutex proxy_processes_mutex;

// Logging function
void log(const std::string& msg, const std::string& level = "INFO") {
    std::lock_guard<std::mutex> lock(log_mutex);
    std::ofstream log_file(log_file_path, std::ios::app);
    time_t now = time(nullptr);
    char* dt = ctime(&now);
    if (dt) dt[strlen(dt)-1] = '\0'; // Remove newline
    log_file << "[" << (dt ? dt : "unknown time") << "] [" << level << "] " << msg << std::endl;
}

// Cleanup SSL
void cleanup_ssl() {
    if (ssl_ctx) {
        SSL_CTX_free(ssl_ctx);
        ssl_ctx = nullptr;
    }
    EVP_cleanup();
}

// Setup SSL with self-signed cert if none exists
void setup_ssl() {
    const std::string cert_path = "cert.pem";
    const std::string key_path = "key.pem";

    if (access(cert_path.c_str(), F_OK) == -1 || access(key_path.c_str(), F_OK) == -1) {
        log("? Certificado SSL não encontrado. Gerando autoassinado...", "WARNING");

        std::string command = "openssl req -x509 -newkey rsa:2048 -keyout " + key_path +
                              " -out " + cert_path + " -days 365 -nodes -subj \"/CN=localhost\"";
        if(system(command.c_str()) != 0) {
            log("? Erro ao gerar certificados autoassinados.", "ERROR");
            exit(1);
        }
        log("?? Certificados autoassinados gerados com sucesso.");
    }

    SSL_library_init();
    OpenSSL_add_all_algorithms();
    SSL_load_error_strings();
    ssl_ctx = SSL_CTX_new(TLS_server_method());
    if (!ssl_ctx) {
        log("? Erro ao configurar contexto SSL.", "ERROR");
        exit(1);
    }

    if(!SSL_CTX_use_certificate_file(ssl_ctx, cert_path.c_str(), SSL_FILETYPE_PEM)){
        log("? Erro ao carregar arquivo de certificado: " + cert_path, "ERROR");
        exit(1);
    }
    if(!SSL_CTX_use_PrivateKey_file(ssl_ctx, key_path.c_str(), SSL_FILETYPE_PEM)){
        log("? Erro ao carregar arquivo de chave privada: " + key_path, "ERROR");
        exit(1);
    }
    log("?? Certificados SSL carregados com sucesso.");
}

// Detect if connection is TLS
bool is_tls_connection(const char* buffer, size_t len) {
    if(len < 1) return false;
    if(buffer[0] == 0x16 && len > 5){
        int major = buffer[1];
        int minor = buffer[2];
        return (major == 3 && (minor >=1 && minor <=4));
    }
    return false;
}
// Detect socks5 handshake
bool is_socks5_connection(const char* buffer, size_t len){
    if(len < 1) return false;
    return (buffer[0] == 0x05);
}
// Detect websocket upgrade request
bool is_websocket_request(const char* buffer, size_t len){
    std::string data(buffer, len);
    if(data.find("GET ") == 0 &&
       (data.find("Upgrade: websocket") != std::string::npos || data.find("upgrade: websocket") != std::string::npos) &&
       (data.find("Connection: Upgrade") != std::string::npos || data.find("connection: upgrade") != std::string::npos)){
        return true;
    }
    return false;
}

// Bidirectional proxy data transfer function
void proxy_data(int src_fd, int dst_fd, SSL* src_ssl = nullptr, SSL* dst_ssl = nullptr){
    char buffer[BUFFER_SIZE];
    ssize_t bytes;
    while(running.load()){
        if(src_ssl){
            bytes = SSL_read(src_ssl, buffer, sizeof(buffer));
            if(bytes <= 0) break;
            if(dst_ssl){
                if(SSL_write(dst_ssl, buffer, bytes) <= 0) break;
            } else {
                if(send(dst_fd, buffer, bytes, 0) <= 0) break;
            }
        } else {
            bytes = recv(src_fd, buffer, sizeof(buffer), 0);
            if(bytes <= 0) break;
            if(dst_ssl){
                if(SSL_write(dst_ssl, buffer, bytes) <= 0) break;
            } else {
                if(send(dst_fd, buffer, bytes, 0) <= 0) break;
            }
        }
    }
}

// Handle websocket proxy (WSS)
void handle_websocket(int client_fd, SSL* client_ssl){
    int ssh_fd = socket(AF_INET, SOCK_STREAM, 0);
    sockaddr_in ssh_addr{};
    ssh_addr.sin_family = AF_INET;
    ssh_addr.sin_port = htons(22);
    inet_pton(AF_INET, "127.0.0.1", &ssh_addr.sin_addr);

    if(connect(ssh_fd, (sockaddr*)&ssh_addr, sizeof(ssh_addr)) < 0){
        log("? Erro ao conectar ao OpenSSH.", "ERROR");
        if(client_ssl) SSL_shutdown(client_ssl);
        close(client_fd);
        return;
    }

    log("?? WebSocket client conectado e redirecionado para OpenSSH.");

    std::thread t1([client_fd, ssh_fd, client_ssl](){
        proxy_data(client_fd, ssh_fd, client_ssl, nullptr);
        shutdown(ssh_fd, SHUT_RDWR);
        close(ssh_fd);
        if(client_ssl) SSL_shutdown(client_ssl);
        close(client_fd);
    });
    std::thread t2([client_fd, ssh_fd, client_ssl](){
        proxy_data(ssh_fd, client_fd, nullptr, client_ssl);
        shutdown(client_fd, SHUT_RDWR);
        close(client_fd);
        close(ssh_fd);
    });

    t1.detach();
    t2.detach();
}

// Handle socks5 proxy
void handle_socks(int client_fd, SSL* client_ssl = nullptr){
    const char* http_ok = "HTTP/1.1 200 OK\r\n\r\n";
    send(client_fd, http_ok, strlen(http_ok), 0);

    char buf[BUFFER_SIZE];
    ssize_t n = recv(client_fd, buf, sizeof(buf), 0);
    if(n <= 0){
        close(client_fd);
        return;
    }
    if(buf[0] != 0x05){
        log("? Protocolo não suportado != SOCKS5", "WARNING");
        close(client_fd);
        return;
    }

    char method_selection[2] = {0x05, 0x00};
    send(client_fd, method_selection, 2, 0);

    n = recv(client_fd, buf, sizeof(buf), 0);
    if(n <= 0){
        close(client_fd);
        return;
    }
    if(buf[1] != 0x01){
        log("? SOCKS comando não suportado.", "WARNING");
        close(client_fd);
        return;
    }

    char resp[10] = {0x05,0x00,0x00,0x01,0,0,0,0,0,22};
    send(client_fd, resp, 10, 0);

    int ssh_fd = socket(AF_INET, SOCK_STREAM, 0);
    sockaddr_in ssh_addr{};
    ssh_addr.sin_family = AF_INET;
    ssh_addr.sin_port = htons(22);
    inet_pton(AF_INET, "127.0.0.1", &ssh_addr.sin_addr);

    if(connect(ssh_fd, (sockaddr*)&ssh_addr, sizeof(ssh_addr)) < 0){
        log("? Erro ao conectar ao OpenSSH.", "ERROR");
        close(client_fd);
        close(ssh_fd);
        return;
    }

    log("?? SOCKS client conectado e redirecionado para OpenSSH.");

    std::thread t1([client_fd, ssh_fd, client_ssl](){
        proxy_data(client_fd, ssh_fd, client_ssl, nullptr);
        shutdown(ssh_fd, SHUT_RDWR);
        close(ssh_fd);
        if(client_ssl) SSL_shutdown(client_ssl);
        close(client_fd);
    });
    std::thread t2([client_fd, ssh_fd, client_ssl](){
        proxy_data(ssh_fd, client_fd, nullptr, client_ssl);
        shutdown(client_fd, SHUT_RDWR);
        close(client_fd);
        close(ssh_fd);
    });
    t1.detach();
    t2.detach();
}

// Handle incoming connections appropriately
void handle_connection(int client_fd){
    char buf[BUFFER_SIZE];
    ssize_t n = recv(client_fd, buf, sizeof(buf), MSG_PEEK | MSG_DONTWAIT);
    if(n <= 0){
        close(client_fd);
        return;
    }

    bool use_tls = is_tls_connection(buf,n);
    SSL* ssl = nullptr;
    if(use_tls){
        ssl = SSL_new(ssl_ctx);
        SSL_set_fd(ssl, client_fd);
        if(SSL_accept(ssl) <= 0){
            log("? Erro na negociação SSL.", "ERROR");
            SSL_free(ssl);
            close(client_fd);
            return;
        }
        char ssl_buf[BUFFER_SIZE];
        n = SSL_peek(ssl, ssl_buf, sizeof(ssl_buf));
        if(n <= 0){
            SSL_shutdown(ssl);
            SSL_free(ssl);
            close(client_fd);
            return;
        }
        if(is_websocket_request(ssl_buf, n)){
            log("Conexão TLS WebSocket detectada.");
            handle_websocket(client_fd, ssl);
            return;
        }
        else if(is_socks5_connection(ssl_buf, n)){
            log("Conexão TLS SOCKS5 detectada.");
            handle_socks(client_fd, ssl);
            return;
        }
        else{
            log("Conexão TLS protocolo desconhecido, fechando.");
            SSL_shutdown(ssl);
            SSL_free(ssl);
            close(client_fd);
            return;
        }
    }
    else{
        if(is_websocket_request(buf,n)){
            log("Conexão WebSocket detectada.");
            handle_websocket(client_fd, nullptr);
            return;
        }
        else if(is_socks5_connection(buf,n)){
            log("Conexão SOCKS5 detectada.");
            handle_socks(client_fd, nullptr);
            return;
        }
        else{
            log("Protocolo desconhecido recebido, fechando conexão.");
            close(client_fd);
            return;
        }
    }
}

// Install firewall rule to allow port traffic (IPv4)
void add_firewall_rule(int port){
    std::string cmd = "iptables -I INPUT -p tcp --dport " + std::to_string(port) + " -j ACCEPT";
    int res = system(cmd.c_str());
    if(res != 0){
        log("Falha ao adicionar regra firewall para porta " + std::to_string(port), "WARNING");
    }
    else {
        log("Regra firewall adicionada para porta " + std::to_string(port));
    }
}

// Signal handler for graceful shutdown
void signal_handler(int signal){
    if(signal == SIGINT || signal == SIGTERM){
        running.store(false);
        log("?? Sinal recebido para encerrar proxy.");
    }
}

// Run proxy server on a port
void run_proxy(int port){
    running.store(true);
    try{
        asio::io_context io_context;
        asio::ip::tcp::acceptor acceptor(io_context);

        asio::ip::tcp::endpoint endpoint(asio::ip::tcp::v4(), port);

        acceptor.open(endpoint.protocol());
        acceptor.set_option(asio::ip::tcp::acceptor::reuse_address(true));
        acceptor.bind(endpoint);
        acceptor.listen(asio::socket_base::max_connections);

        log("?? Proxy iniciado na porta " + std::to_string(port));
        add_firewall_rule(port);

        while(running.load()){
            asio::ip::tcp::socket socket(io_context);
            try{
                acceptor.accept(socket);
            }
            catch(const std::exception& e){
                if(!running.load()) break;
                log(std::string("? Erro ao aceitar conexão: ") + e.what(), "ERROR");
                continue;
            }
            std::thread([sock = std::move(socket)]() mutable {
                int client_fd = sock.native_handle();
                handle_connection(client_fd);
            }).detach();
        }
        acceptor.close();
    }catch(const std::exception& e){
        log(std::string("? Erro no proxy na porta ") + std::to_string(port) + ": " + e.what(), "ERROR");
    }
    log("?? Proxy encerrado na porta " + std::to_string(port));
}

// Launch proxy as background process and return PID
pid_t launch_proxy_process(int port){
    pid_t pid = fork();
    if(pid == -1){
        log("Erro ao criar processo proxy para porta " + std::to_string(port), "ERROR");
        return -1;
    }
    if(pid == 0){
        setup_ssl();
        run_proxy(port);
        cleanup_ssl();
        exit(0);
    }
    log("Proxy iniciado na porta " + std::to_string(port) + ", PID " + std::to_string(pid));
    return pid;
}

// Load saved ports & pids from a state file
void load_persisted_proxies(std::map<int, pid_t>& proxies){
    std::ifstream file("proxy_state.txt");
    if(!file.is_open()) return;
    int port; pid_t pid;
    while(file >> port >> pid){
        // Check if process alive
        if(kill(pid, 0) == 0){
            proxies[port] = pid;
        }
    }
}
// Save current proxies to state file
void save_persisted_proxies(const std::map<int, pid_t>& proxies){
    std::ofstream file("proxy_state.txt", std::ios::trunc);
    if(!file.is_open()) return;
    for(const auto& p : proxies){
        file << p.first << " " << p.second << "\n";
    }
}

void interactive_menu(){
    load_persisted_proxies(proxy_processes);
    while(true){
        system("clear");
        {
            std::lock_guard<std::mutex> lock(proxy_processes_mutex);
            std::cout << "=== Proxies Ativos ===\n";
            if(proxy_processes.empty()){
                std::cout << "(Nenhum proxy ativo)\n";
            }
            else{
                for(const auto& p : proxy_processes){
                    std::cout << "Porta " << p.first << " - PID " << p.second << "\n";
                }
            }
            std::cout << "======================\n";
        }
        std::cout << "1) Abrir nova porta\n2) Fechar proxy numa porta\n3) Sair\nEscolha: ";
        int choice;
        if(!(std::cin >> choice)){
            std::cin.clear();
            std::cin.ignore(10000, '\n');
            continue;
        }
        if(choice == 1){
            std::cout << "Porta para abrir: ";
            int port;
            if(!(std::cin >> port) || port <=0 || port > 65535){
                std::cout << "Porta inválida! Pressione ENTER...";
                std::cin.ignore();
                std::cin.get();
                continue;
            }
            {
                std::lock_guard<std::mutex> lock(proxy_processes_mutex);
                if(proxy_processes.count(port)){
                    std::cout << "Porta já aberta! Pressione ENTER...";
                    std::cin.ignore();
                    std::cin.get();
                    continue;
                }
            }
            pid_t pid = launch_proxy_process(port);
            if(pid == -1){
                std::cout << "Erro ao iniciar proxy. Pressione ENTER...";
                std::cin.ignore();
                std::cin.get();
            }
            else{
                std::lock_guard<std::mutex> lock(proxy_processes_mutex);
                proxy_processes[port] = pid;
                save_persisted_proxies(proxy_processes);
                std::cout << "Proxy iniciado na porta " << port << " (PID " << pid << "). Pressione ENTER...";
                std::cin.ignore();
                std::cin.get();
            }
        }
        else if(choice == 2){
            std::cout << "Porta para fechar: ";
            int port;
            if(!(std::cin >> port)){
                std::cout << "Entrada inválida. Pressione ENTER...";
                std::cin.ignore();
                std::cin.get();
                continue;
            }
            pid_t pid = -1;
            {
                std::lock_guard<std::mutex> lock(proxy_processes_mutex);
                if(proxy_processes.count(port)){
                    pid = proxy_processes[port];
                }
            }
            if(pid == -1){
                std::cout << "Nenhum proxy aberto nessa porta. Pressione ENTER...";
                std::cin.ignore();
                std::cin.get();
                continue;
            }
            // Use SIGINT for gentle stop instead of SIGTERM
            kill(pid, SIGINT);
            waitpid(pid, nullptr, 0);
            {
                std::lock_guard<std::mutex> lock(proxy_processes_mutex);
                proxy_processes.erase(port);
                save_persisted_proxies(proxy_processes);
            }
            std::cout << "Proxy na porta " << port << " encerrado. Pressione ENTER...";
            std::cin.ignore();
            std::cin.get();
        }
        else if(choice == 3){
            std::cout << "Saindo. Proxies continuarão rodando em background.\n";
            break;
        }
        else{
            std::cout << "Opção inválida. Pressione ENTER...";
            std::cin.ignore();
            std::cin.get();
        }
    }
}

int main(int argc, char* argv[]){
    signal(SIGINT, signal_handler);
    signal(SIGTERM, signal_handler);
    if(argc == 2){
        int port = std::stoi(argv[1]);
        setup_ssl();
        run_proxy(port);
        cleanup_ssl();
        return 0;
    }
    interactive_menu();
    return 0;
}
