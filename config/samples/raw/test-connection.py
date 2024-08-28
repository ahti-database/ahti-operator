import libsql_experimental as libsql

remote_db_url = "http://localhost:8082"
db_auth_token = input("token: ")

conn = libsql.connect("local.db", sync_url=remote_db_url, auth_token=db_auth_token)
conn.execute("CREATE TABLE IF NOT EXISTS users (id INTEGER);")
conn.execute("INSERT INTO users(id) VALUES (1);")
conn.commit()

print(conn.execute("select * from users").fetchall())
