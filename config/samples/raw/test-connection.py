import libsql_experimental as libsql

remote_db_url = "http://localhost:8080"
db_auth_token = "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9.e30.V9dlFGLOZj8whq9OveT23d3MaQ-nwN7fDvsh8sNonDm3Iby7cT_zcygFn1-a13z7cwS8cBzJIg7F_Vo07I0hAw"

conn = libsql.connect("local.db", sync_url=remote_db_url, auth_token=db_auth_token)
conn.execute("CREATE TABLE IF NOT EXISTS users (id INTEGER);")
conn.execute("INSERT INTO users(id) VALUES (1);")
conn.commit()

print(conn.execute("select * from users").fetchall())
