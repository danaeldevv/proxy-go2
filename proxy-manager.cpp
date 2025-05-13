import asyncio
import websockets
import socket
import threading
import sys
import struct

OPENSSH_HOST = '127.0.0.1'
OPENSSH_PORT = 22

class ProxyServer:
    def __init__(self):
        self.open_ports = {}

    async def proxy_bidirectional(self, reader1, writer1, reader2, writer2):
        async def pipe(reader, writer):
            try:
                while True:
                    data = await reader.read(4096)
                    if not data:
                        break
                    writer.write(data)
                    await writer.drain()
            except Exception:
                pass
            finally:
                try:
                    writer.close()
                    await writer.wait_closed()
                except Exception:
                    pass

        await asyncio.gather(
            pipe(reader1, writer2),
            pipe(reader2, writer1)
        )

    async def handle_websocket(self, websocket, path):
        ip = websocket.remote_address[0]
        print(f"[WebSocket] Connection from {ip}")

        try:
            reader_ssh, writer_ssh = await asyncio.open_connection(OPENSSH_HOST, OPENSSH_PORT)

            async def ws_to_ssh():
                try:
                    async for message in websocket:
                        if isinstance(message, str):
                            data = message.encode('utf-8')
                        else:
                            data = message
                        writer_ssh.write(data)
                        await writer_ssh.drain()
                except Exception:
                    pass
                finally:
                    try:
                        writer_ssh.close()
                        await writer_ssh.wait_closed()
                    except Exception:
                        pass

            async def ssh_to_ws():
                try:
                    while True:
                        data = await reader_ssh.read(4096)
                        if not data:
                            break
                        await websocket.send(data)
                except Exception:
                    pass
                finally:
                    try:
                        await websocket.close()
                    except Exception:
                        pass

            await asyncio.gather(ws_to_ssh(), ssh_to_ws())

        except Exception as e:
            print(f"[WebSocket] Proxy error: {e}")

        print(f"[WebSocket] Connection closed: {ip}")

    async def handle_socks(self, reader, writer):
        addr = writer.get_extra_info('peername')
        ip = addr[0] if addr else 'unknown'
        print(f"[SOCKS] Connection from {ip}")

        try:
            data = await reader.read(2)
            if len(data) < 2:
                raise Exception("Invalid SOCKS handshake")
            ver, nmethods = data[0], data[1]
            if ver != 0x05:
                raise Exception(f"Invalid SOCKS version: {ver}")

            methods = await reader.read(nmethods)
            writer.write(b'\x05\x00')  # No authentication
            await writer.drain()

            data = await reader.read(4)
            if len(data) < 4:
                raise Exception("Invalid SOCKS request")
            ver, cmd, rsv, atyp = data[0], data[1], data[2], data[3]
            if ver != 0x05:
                raise Exception("Invalid SOCKS version in request")
            if cmd != 0x01:  # Only CONNECT supported
                writer.write(b'\x05\x07\x00\x01\x00\x00\x00\x00\x00\x00')  # Command not supported
                await writer.drain()
                writer.close()
                await writer.wait_closed()
                return

            if atyp == 0x01:
                addr_raw = await reader.read(4)
                dest_addr = socket.inet_ntoa(addr_raw)
            elif atyp == 0x03:
                length = await reader.read(1)
                length = length[0]
                domain = await reader.read(length)
                dest_addr = domain.decode()
            elif atyp == 0x04:
                addr_raw = await reader.read(16)
                dest_addr = socket.inet_ntop(socket.AF_INET6, addr_raw)
            else:
                writer.close()
                await writer.wait_closed()
                return

            dest_port_raw = await reader.read(2)
            dest_port = struct.unpack('>H', dest_port_raw)[0]

            print(f"[SOCKS] CONNECT requested to {dest_addr}:{dest_port} (forced to {OPENSSH_HOST}:{OPENSSH_PORT})")

            resp = b'\x05\x00\x00\x01' + socket.inet_aton(OPENSSH_HOST) + struct.pack('>H', OPENSSH_PORT)
            writer.write(resp)
            await writer.drain()

            reader_ssh, writer_ssh = await asyncio.open_connection(OPENSSH_HOST, OPENSSH_PORT)
            await self.proxy_bidirectional(reader, writer, reader_ssh, writer_ssh)

        except Exception as e:
            print(f"[SOCKS] Proxy error: {e}")
            try:
                writer.close()
                await writer.wait_closed()
            except Exception:
                pass

        print(f"[SOCKS] Connection closed: {ip}")

    def start_websocket_server(self, port):
        async def server_coro():
            ws_server = await websockets.serve(self.handle_websocket, "0.0.0.0", port)
            print(f"[INFO] WebSocket server listening on port {port}")
            await ws_server.wait_closed()

        def run_loop():
            loop = asyncio.new_event_loop()
            asyncio.set_event_loop(loop)
            loop.run_until_complete(server_coro())

        ws_thread = threading.Thread(target=run_loop, daemon=True)
        ws_thread.start()
        self.open_ports[port]["ws_thread"] = ws_thread

    def start_socks_server(self, port):
        async def server_coro():
            server = await asyncio.start_server(self.handle_socks, '0.0.0.0', port)
            print(f"[INFO] SOCKS server listening on port {port}")
            async with server:
                await server.serve_forever()

        def run_loop():
            loop = asyncio.new_event_loop()
            asyncio.set_event_loop(loop)
            loop.run_until_complete(server_coro())

        socks_thread = threading.Thread(target=run_loop, daemon=True)
        socks_thread.start()
        self.open_ports[port]["socks_thread"] = socks_thread

    def open_port(self, port):
        if port in self.open_ports:
            print(f"[WARN] Port {port} is already open.")
            return
        print(f"[INFO] Opening port {port} for WebSocket and SOCKS ...")
        self.open_ports[port] = {}
        self.start_websocket_server(port)
        self.start_socks_server(port)

    def close_port(self, port):
        if port not in self.open_ports:
            print(f"[WARN] Port {port} is not open by proxy.")
            return
        print(f"[INFO] Closing proxy on port {port} (clean shutdown not implemented).")
        del self.open_ports[port]

    def show_open_ports(self):
        if not self.open_ports:
            print("[INFO] No open ports.")
        else:
            print("[INFO] Open ports:")
            for port in self.open_ports.keys():
                print(f" - Port {port}")

    def menu(self):
        while True:
            print("\n=== Multiprotocol Proxy with OpenSSH ===")
            self.show_open_ports()
            print("1. Open port")
            print("2. Close port")
            print("3. Exit")
            choice = input("Choose an option: ").strip()

            if choice == '1':
                port_str = input("Enter port to open: ").strip()
                if not port_str.isdigit():
                    print("[ERROR] Invalid port number.")
                    continue
                port = int(port_str)
                if port < 1 or port > 65535:
                    print("[ERROR] Port number out of range (1-65535).")
                    continue
                self.open_port(port)

            elif choice == '2':
                port_str = input("Enter port to close: ").strip()
                if not port_str.isdigit():
                    print("[ERROR] Invalid port number.")
                    continue
                port = int(port_str)
                self.close_port(port)

            elif choice == '3':
                print("[INFO] Exiting proxy.")
                sys.exit(0)
            else:
                print("[ERROR] Invalid option. Try again.")

if __name__ == "__main__":
    proxy_server = ProxyServer()
    try:
        proxy_server.menu()
    except KeyboardInterrupt:
        print("\n[INFO] Interrupted by user, exiting.")
        sys.exit(0)

