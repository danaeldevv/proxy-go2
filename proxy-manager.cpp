#include <iostream>
#include <fstream>
#include <thread>
#include <unistd.h>
#include <csignal>
#include <cstring>
#include <netinet/in.h>
#include <arpa/inet.h>
#include <fcntl.h>
#include <sys/epoll.h>
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
#include <libevent.h>
#include <sys/resource.h>
#include <sys/sysinfo.h>
#include <sys/time.h>

// Constants
#define MAX_EVENTS 1024
#define BUFFER_SIZE 8192

// Globals
int server_fd = -1;
std::atomic<bool> running(true);
const std::string pid_file_path = "/var/run/proxyws.pid";
const std::string log_file_path = "/var/log/proxyws.log";

std::mutex log_mutex;
std::mutex ports_mutex;
std::map<int, bool> open_ports; // Stores open ports with value 'true' if proxy running
SSL_CTX *ssl_ctx;

void log(const std::string& msg, const std::string& level = "INFO") {
    std::lock_guard<std::mutex> lock(log_mutex);
    std::ofstream log_file(log_file_path, std::ios::app);
    time_t now = time(0);
    char* dt = ctime(&now);
    dt[strlen(dt) - 1] = '\0'; // Remove newline
    log_file << "[" << dt << "] [" << level << "] " << msg << std::endl;
}

// Set fd to nonblocking mode
int set_non_blocking(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags == -1) return -1;
    return fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

// Cleanup SSL resources
void cleanup_ssl() {
    if (ssl_ctx) {
        SSL_CTX_free(ssl_ctx);
        ssl_ctx = nullptr;
    }
    EVP_cleanup();
}

// Initialize SSL context for TLS
void setup_ssl() {
    SSL_library_init();
    OpenSSL_add_all_algorithms();
    SSL_load_error_strings();
    ssl_ctx = SSL_CTX_new(TLS_server_method());
    if (!ssl_ctx) {
        log("❌ Erro ao configurar SSL.", "ERROR");
        exit(1);
    }
    if (!SSL_CTX_use_certificate_file(ssl_ctx, "cert.pem", SSL_FILETYPE_PEM) ||
        !SSL_CTX_use_PrivateKey_file(ssl_ctx, "key.pem", SSL_FILETYPE_PEM)) {
        log("❌ Erro ao carregar certificado SSL.", "ERROR");
        exit(1);
    }
}

// Helper: check if connection data indicates TLS (WSS)
bool is_tls_connection(const char* buffer, size_t len) {
    if(len < 1) return false;
    // TLS handshake record type is 0x16 and version is 0x0301/0303 etc.
    // Check first byte is 0x16 (Handshake), and version bytes next, also enough length
    if (buffer[0] == 0x16 && len > 5) {
        int major = buffer[1];
        int minor = buffer[2];
        if (major == 3 && (minor == 1 || minor == 2 || minor == 3 || minor == 4))
            return true;
    }
    return false;
}

// Helper: check if data seems SOCKS5 handshake
bool is_socks5_connection(const char* buffer, size_t len) {
    if (len < 1) return false;
    return (buffer[0] == 0x05);
}

// Helper: check if data is HTTP WebSocket upgrade request (simple check)
bool is_websocket_request(const char* buffer, size_t len) {
    std::string data(buffer, len);
    if (data.find("GET ") == 0 &&
        (data.find("Upgrade: websocket") != std::string::npos || data.find("upgrade: websocket") != std::string::npos) &&
        (data.find("Connection: Upgrade") != std::string::npos || data.find("connection: upgrade") != std::string::npos)) {
        return true;
    }
    return false;
}

// Copy data bidirectionally between sockets or SSLs
void proxy_data(int src_fd, int dst_fd, SSL* src_ssl = nullptr, SSL* dst_ssl = nullptr) {
    char buffer[BUFFER_SIZE];
    ssize_t bytes;
    while (running) {
        if (src_ssl) {
            bytes = SSL_read(src_ssl, buffer, sizeof(buffer));
            if (bytes <= 0) break;
            if (dst_ssl) {
                if (SSL_write(dst_ssl, buffer, bytes) <= 0) break;
            } else {
                if (send(dst_fd, buffer, bytes, 0) <= 0) break;
            }
        } else {
            bytes = recv(src_fd, buffer, sizeof(buffer), 0);
            if (bytes <= 0) break;
            if (dst_ssl) {
                if (SSL_write(dst_ssl, buffer, bytes) <= 0) break;
            } else {
                if (send(dst_fd, buffer, bytes, 0) <= 0) break;
            }
        }
    }
}

// Handle WebSocket proxy to SSH (with optional SSL)
void handle_websocket(int client_fd, SSL* client_ssl) {
    int ssh_fd = socket(AF_INET, SOCK_STREAM, 0);
    sockaddr_in ssh_addr{};
    ssh_addr.sin_family = AF_INET;
    ssh_addr.sin_port = htons(22);
    inet_pton(AF_INET, "127.0.0.1", &ssh_addr.sin_addr);

    if (connect(ssh_fd, (sockaddr*)&ssh_addr, sizeof(ssh_addr)) < 0) {
        log("❌ Erro ao conectar ao OpenSSH.", "ERROR");
        if (client_ssl) SSL_shutdown(client_ssl);
        close(client_fd);
        return;
    }

    log("🔗 WebSocket client conectado e redirecionado para OpenSSH.");

    std::thread t1([client_fd, ssh_fd, client_ssl]() {
        proxy_data(client_fd, ssh_fd, client_ssl, nullptr);
        shutdown(ssh_fd, SHUT_RDWR);
        close(ssh_fd);
        if (client_ssl) SSL_shutdown(client_ssl);
        close(client_fd);
    });

    std::thread t2([client_fd, ssh_fd, client_ssl]() {
        proxy_data(ssh_fd, client_fd, nullptr, client_ssl);
        shutdown(client_fd, SHUT_RDWR);
        close(client_fd);
        close(ssh_fd);
    });

    t1.detach();
    t2.detach();
}

// Handle SOCKS5 proxy connection to SSH
void handle_socks(int client_fd, SSL* client_ssl = nullptr) {
    // Respond HTTP 200 OK before SOCKS handshake (per original)
    const char* http_ok = "HTTP/1.1 200 OK\r\n\r\n";
    send(client_fd, http_ok, strlen(http_ok), 0);

    // Read initial handshake
    char buf[BUFFER_SIZE];
    ssize_t n = recv(client_fd, buf, sizeof(buf), 0);
    if (n <= 0) {
        close(client_fd);
        return;
    }

    if (buf[0] != 0x05) { // Not SOCKS5
        log("❌ Protocolo nao suportado != SOCKS5", "WARNING");
        close(client_fd);
        return;
    }

    // Send no-auth method select
    char method_selection[2] = {0x05, 0x00};
    send(client_fd, method_selection, 2, 0);

    // Read SOCKS5 request
    n = recv(client_fd, buf, sizeof(buf), 0);
    if (n <= 0) {
        close(client_fd);
        return;
    }

    // Only support CONNECT command (0x01)
    if (buf[1] != 0x01) {
        log("❌ SOCKS comando nao suportado.", "WARNING");
        close(client_fd);
        return;
    }

    // Build SOCKS response success with bind address 0.0.0.0 port 22
    char resp[10] = {0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 22};
    send(client_fd, resp, 10, 0);

    int ssh_fd = socket(AF_INET, SOCK_STREAM, 0);
    sockaddr_in ssh_addr{};
    ssh_addr.sin_family = AF_INET;
    ssh_addr.sin_port = htons(22);
    inet_pton(AF_INET, "127.0.0.1", &ssh_addr.sin_addr);

    if (connect(ssh_fd, (sockaddr*)&ssh_addr, sizeof(ssh_addr)) < 0) {
        log("❌ Erro ao conectar ao OpenSSH.", "ERROR");
        close(client_fd);
        close(ssh_fd);
        return;
    }

    log("🔗 SOCKS client conectado e redirecionado para OpenSSH.");

    std::thread t1([client_fd, ssh_fd, client_ssl]() {
        proxy_data(client_fd, ssh_fd, client_ssl, nullptr);
        shutdown(ssh_fd, SHUT_RDWR);
        close(ssh_fd);
        if (client_ssl) SSL_shutdown(client_ssl);
        close(client_fd);
    });

    std::thread t2([client_fd, ssh_fd, client_ssl]() {
        proxy_data(ssh_fd, client_fd, nullptr, client_ssl);
        shutdown(client_fd, SHUT_RDWR);
        close(client_fd);
        close(ssh_fd);
    });

    t1.detach();
    t2.detach();
}

// Handle a new client connection: detect if TLS, websocket or socks, dispatch accordingly
void handle_connection(int client_fd) {
    // Peek at initial bytes without removing from socket buffer
    char buf[BUFFER_SIZE];
    ssize_t n = recv(client_fd, buf, sizeof(buf), MSG_PEEK | MSG_DONTWAIT); 
    if (n <= 0) {
        close(client_fd);
        return;
    }

    bool use_tls = is_tls_connection(buf, n);

    SSL* ssl = nullptr;
    if (use_tls) {
        ssl = SSL_new(ssl_ctx);
        SSL_set_fd(ssl, client_fd);
        if (SSL_accept(ssl) <= 0) {
            log("❌ Erro na negociação SSL.", "ERROR");
            SSL_free(ssl);
            close(client_fd);
            return;
        }

        // Now peek decrypted bytes from SSL
        char ssl_buf[BUFFER_SIZE];
        n = SSL_peek(ssl, ssl_buf, sizeof(ssl_buf));
        if (n <= 0) {
            SSL_shutdown(ssl);
            SSL_free(ssl);
            close(client_fd);
            return;
        }

        if (is_websocket_request(ssl_buf, n)) {
            log("Conexão TLS WebSocket detectada.");
            handle_websocket(client_fd, ssl);
            return;
        } else if (is_socks5_connection(ssl_buf, n)) {
            log("Conexão TLS SOCKS5 detectada.");
            handle_socks(client_fd, ssl);
            return;
        } else {
            log("Conexão TLS protocolo desconhecido, fechando.");
            SSL_shutdown(ssl);
            SSL_free(ssl);
            close(client_fd);
            return;
        }
    } else {
        if (is_websocket_request(buf, n)) {
            log("Conexão WebSocket detectada.");
            handle_websocket(client_fd, nullptr);
            return;
        } else if (is_socks5_connection(buf, n)) {
            log("Conexão SOCKS5 detectada.");
            handle_socks(client_fd, nullptr);
            return;
        } else {
            log("Protocolo desconhecido recebido, fechando conexao.");
            close(client_fd);
            return;
        }
    }
}

// Returns CPU usage % approximated for current process over interval
float get_cpu_usage() {
    static long long last_total_user = 0, last_total_system = 0, last_proc_user = 0, last_proc_system = 0;
    FILE* file = fopen("/proc/stat", "r");
    if (!file) return 0.0f;

    long long total_user, total_nice, total_system, total_idle, total_iowait, total_irq, total_softirq, total_steal;
    fscanf(file, "cpu %lld %lld %lld %lld %lld %lld %lld %lld", 
        &total_user, &total_nice, &total_system, &total_idle, &total_iowait, &total_irq, &total_softirq, &total_steal);
    fclose(file);
    long long total = total_user + total_nice + total_system + total_idle + total_iowait + total_irq + total_softirq + total_steal;

    pid_t pid = getpid();
    char stat_path[64];
    snprintf(stat_path, sizeof(stat_path), "/proc/%d/stat", pid);

    file = fopen(stat_path, "r");
    if (!file) return 0.0f;

    long utime, stime;
    char buffer[1024];
    fgets(buffer, sizeof(buffer), file);
    fclose(file);
    sscanf(buffer, "%*d %*s %*c %*d %*d %*d %*d %*d %*u %*u %*u %*u %*u %ld %ld", &utime, &stime);

    long long total_proc = utime + stime;
    long long total_all = total;

    static long long last_total_all = 0, last_total_proc = 0;
    long long total_diff = total_all - last_total_all;
    long long proc_diff = total_proc - last_total_proc;

    float cpu_usage = 0.0f;
    if (total_diff != 0) {
        cpu_usage = (proc_diff * 100.0) / total_diff;
    }

    last_total_all = total_all;
    last_total_proc = total_proc;

    return cpu_usage;
}

// Returns memory usage in MB for current process
float get_mem_usage() {
    pid_t pid = getpid();
    char status_path[64];
    snprintf(status_path, sizeof(status_path), "/proc/%d/status", pid);

    std::ifstream status_file(status_path);
    std::string line;
    while(std::getline(status_file, line)) {
        if(line.find("VmRSS:") == 0) {
            std::istringstream iss(line);
            std::string key, val, unit;
            iss >> key >> val >> unit;
            float mem_kb = std::stof(val);
            return mem_kb / 1024.0f; // Convert to MB
        }
    }
    return 0.0f;
}

// Thread-safe print with CPU/memory usage and open ports info
void show_status() {
    std::lock_guard<std::mutex> lock(ports_mutex);
    float cpu = get_cpu_usage();
    float mem = get_mem_usage();
    std::cout << "=== Proxy Status ===\n";
    std::cout << "Uso CPU: " << cpu << "% | Uso Memória: " << mem << " MB\n";
    std::cout << "Portas abertas:\n";
    for (const auto& p : open_ports) {
        std::cout << " - Porta " << p.first << "\n";
    }
    std::cout << "====================\n";
}

void run_proxy(int port) {
    // Mark port as open
    {
        std::lock_guard<std::mutex> lock(ports_mutex);
        open_ports[port] = true;
    }

    int server_fd_local = socket(AF_INET, SOCK_STREAM, 0);
    if (server_fd_local < 0) {
        log("❌ Erro ao criar socket do servidor.", "ERROR");
        return;
    }

    int opt = 1;
    setsockopt(server_fd_local, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));

    sockaddr_in server_addr{};
    server_addr.sin_family = AF_INET;
    server_addr.sin_addr.s_addr = INADDR_ANY;
    server_addr.sin_port = htons(port);

    if (bind(server_fd_local, (sockaddr*)&server_addr, sizeof(server_addr)) < 0) {
        log("❌ Erro ao vincular porta.", "ERROR");
        close(server_fd_local);
        return;
    }

    if (listen(server_fd_local, SOMAXCONN) < 0) {
        log("❌ Erro ao escutar conexões.", "ERROR");
        close(server_fd_local);
        return;
    }

    set_non_blocking(server_fd_local);

    log("🟢 Proxy iniciado na porta " + std::to_string(port));

    int epoll_fd = epoll_create1(0);
    if (epoll_fd == -1) {
        log("❌ Falha ao criar epoll.", "ERROR");
        close(server_fd_local);
        return;
    }

    epoll_event event{}, events[MAX_EVENTS];
    event.events = EPOLLIN;
    event.data.fd = server_fd_local;
    epoll_ctl(epoll_fd, EPOLL_CTL_ADD, server_fd_local, &event);

    while (running.load()) {
        int nfds = epoll_wait(epoll_fd, events, MAX_EVENTS, 1000);
        if (nfds == -1) {
            if (errno == EINTR) continue;
            log("❌ epoll_wait falhou.", "ERROR");
            break;
        }

        for (int i = 0; i < nfds; ++i) {
            if(events[i].data.fd == server_fd_local) {
                sockaddr_in client_addr;
                socklen_t client_len = sizeof(client_addr);
                int client_fd = accept(server_fd_local, (sockaddr*)&client_addr, &client_len);
                if(client_fd >= 0) {
                    set_non_blocking(client_fd);
                    std::thread(handle_connection, client_fd).detach();
                }
            }
        }
    }

    close(epoll_fd);
    close(server_fd_local);

    {
        std::lock_guard<std::mutex> lock(ports_mutex);
        open_ports.erase(port);
    }
    log("🔴 Proxy encerrado na porta " + std::to_string(port));
}

void signal_handler(int signal) {
    if (signal == SIGINT || signal == SIGTERM) {
        running = false;
        log("🔴 Sinal recebido para encerrar proxy.");
    }
}

// Interactive menu
void interactive_menu() {
    while (running.load()) {
        system("clear");
        show_status();
        std::cout << "1) Abrir nova porta\n";
        std::cout << "2) Encerrar proxy numa porta\n";
        std::cout << "3) Sair\n";
        std::cout << "Escolha uma opcao: ";
        int choice;
        std::cin >> choice;
        if (choice == 1) {
            std::cout << "Digite a porta para abrir: ";
            int port; std::cin >> port;
            if (port > 0 && port <= 65535) {
                std::thread(run_proxy, port).detach();
                std::cout << "Proxy iniciado na porta " << port << ". Pressione ENTER para continuar...";
                std::cin.ignore(); std::cin.get();
            } else {
                std::cout << "Porta invalida! Pressione ENTER para continuar...";
                std::cin.ignore(); std::cin.get();
            }
        } else if (choice == 2) {
            std::cout << "Digite a porta para encerrar: ";
            int port; std::cin >> port;
            std::lock_guard<std::mutex> lock(ports_mutex);
            if(open_ports.count(port)) {
                std::string cmd = "fuser -k " + std::to_string(port) + "/tcp";
                system(cmd.c_str());
                std::cout << "Proxy da porta " << port << " encerrado. Pressione ENTER...";
                open_ports.erase(port);
                std::cin.ignore(); std::cin.get();
            } else {
                std::cout << "Porta nao esta aberta. Pressione ENTER...";
                std::cin.ignore(); std::cin.get();
            }
        } else if (choice == 3) {
            running = false;
            break;
        } else {
            std::cout << "Opcao invalida. Pressione ENTER...";
            std::cin.ignore(); std::cin.get();
        }
    }
}

int main() {
    signal(SIGINT, signal_handler);
    signal(SIGTERM, signal_handler);

    setup_ssl();

    std::thread menu_thread(interactive_menu);
    menu_thread.join();

    cleanup_ssl();

    return 0;
}

