#!/usr/bin/env python3
import sys
import socket
import threading
import select
import errno
import signal

SSH_HOST = '127.0.0.1'
SSH_PORT = 22
BUFFER_SIZE = 8192

def handle_client(client_sock, client_addr):
    try:
        # Peek first few bytes from client to detect protocol
        client_sock.settimeout(5)
        initial_data = client_sock.recv(1024, socket.MSG_PEEK)
        if not initial_data:
            client_sock.close()
            return

        # Simple detection: SOCKS5 starts with 0x05, HTTP (WS handshake) starts with GET or other methods
        if initial_data.startswith(b'\x05'):
            # SOCKS5 handshake
            client_sock.recv(1024)  # consume the handshake completely (simplified)
            # Respond with HTTP/1.1 101 switching protocols (per user request)
            response = b"HTTP/1.1 101 Switching Protocols\r\n\r\n"
            client_sock.sendall(response)
        else:
            # Assume HTTP/WebSocket handshake
            # Read full HTTP header (simplified: read until double \r\n)
            request_buffer = b""
            while b"\r\n\r\n" not in request_buffer:
                chunk = client_sock.recv(1024)
                if not chunk:
                    break
                request_buffer += chunk
            # Send HTTP/1.1 200 OK response as requested
            response = b"HTTP/1.1 200 OK\r\n\r\n"
            client_sock.sendall(response)

        # Connect to OpenSSH server
        ssh_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        ssh_sock.connect((SSH_HOST, SSH_PORT))

        # Forward data between client and ssh_sock bi-directionally
        sockets = [client_sock, ssh_sock]
        while True:
            rlist, _, _ = select.select(sockets, [], [])
            for s in rlist:
                data = b''
                try:
                    data = s.recv(BUFFER_SIZE)
                except socket.error as e:
                    if e.errno != errno.ECONNRESET:
                        raise
                if not data:
                    # Connection closed
                    client_sock.close()
                    ssh_sock.close()
                    return
                if s is client_sock:
                    ssh_sock.sendall(data)
                else:
                    client_sock.sendall(data)

    except Exception as e:
        # On any error, close sockets and exit thread
        try:
            client_sock.close()
        except:
            pass
        try:
            ssh_sock.close()
        except:
            pass

def run_proxy(port):
    server_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server_sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server_sock.bind(('0.0.0.0', port))
    server_sock.listen(10000)
    print(f"Proxy server listening on port {port}")

    while True:
        try:
            client_sock, client_addr = server_sock.accept()
        except OSError:
            # Socket closed externally
            break
        thread = threading.Thread(target=handle_client, args=(client_sock, client_addr))
        thread.daemon = True
        thread.start()

def signal_handler(signum, frame):
    print("Shutting down proxy server...")
    sys.exit(0)

if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("Usage: proxy_server.py <port>")
        sys.exit(1)
    
    try:
        proxy_port = int(sys.argv[1])
        
        # Set up signal handling in the main thread
        signal.signal(signal.SIGINT, signal_handler)
        signal.signal(signal.SIGTERM, signal_handler)

        # Run the proxy server in a separate thread
        proxy_thread = threading.Thread(target=run_proxy, args=(proxy_port,))
        proxy_thread.daemon = True
        proxy_thread.start()
        print("Proxy server is running in the background.")
        
        # Keep the main thread alive to allow the user to use the terminal
        while True:
            pass  # You can replace this with any other logic you want to run in the main thread

    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)
