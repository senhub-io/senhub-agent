import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.PrintWriter;
import java.nio.charset.StandardCharsets;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.ResultSet;
import java.sql.ResultSetMetaData;
import java.sql.SQLException;
import java.sql.Statement;

/**
 * Long-lived JDBC bridge for IBM i, driven by line-oriented stdin/stdout.
 *
 * Protocol (1 line in, 1 line out, UTF-8, newline-delimited):
 *   - Startup handshake: the runner writes {"ok":true,"event":"ready"} once
 *     the JDBC connection is established.
 *   - Request: a single SQL statement on one line (no embedded newlines).
 *     An empty line is interpreted as "shutdown": the runner closes the
 *     connection and exits cleanly.
 *   - Response: a single line of JSON, either
 *       {"ok":true,"columns":[...],"rows":[[...], ...]}
 *     or
 *       {"ok":false,"error":"..."}
 *
 * Credentials are passed via environment variables (PUB400_HOST, PUB400_USER,
 * PUB400_PASSWORD) — never on the command line, never on stdin.
 *
 * All column values are serialized as JSON strings (or null). Type coercion
 * is the Go side's responsibility — this keeps the bridge stateless and
 * format-agnostic.
 */
public class Jt400Runner {

    public static void main(String[] args) throws Exception {
        String host = envOr("PUB400_HOST", "pub400.com");
        String user = System.getenv("PUB400_USER");
        String password = System.getenv("PUB400_PASSWORD");
        if (user == null || password == null) {
            System.err.println("PUB400_USER and PUB400_PASSWORD are required");
            System.exit(2);
        }

        PrintWriter out = new PrintWriter(new java.io.OutputStreamWriter(System.out, StandardCharsets.UTF_8), true);
        BufferedReader in = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.UTF_8));

        String url = "jdbc:as400://" + host + ";naming=sql;errors=full;prompt=false";
        Connection conn;
        try {
            conn = DriverManager.getConnection(url, user, password);
        } catch (SQLException e) {
            out.println("{\"ok\":false,\"error\":" + jsonString("connect: " + e.getMessage()) + "}");
            System.exit(1);
            return;
        }

        out.println("{\"ok\":true,\"event\":\"ready\"}");

        String line;
        while ((line = in.readLine()) != null) {
            if (line.isEmpty()) {
                break;
            }
            try (Statement st = conn.createStatement();
                 ResultSet rs = st.executeQuery(line)) {
                out.println(serializeResultSet(rs));
            } catch (SQLException e) {
                out.println("{\"ok\":false,\"error\":" + jsonString(e.getMessage()) + "}");
            }
        }

        conn.close();
    }

    private static String serializeResultSet(ResultSet rs) throws SQLException {
        ResultSetMetaData md = rs.getMetaData();
        int n = md.getColumnCount();

        StringBuilder sb = new StringBuilder(256);
        sb.append("{\"ok\":true,\"columns\":[");
        for (int i = 1; i <= n; i++) {
            if (i > 1) sb.append(',');
            sb.append(jsonString(md.getColumnLabel(i)));
        }
        sb.append("],\"rows\":[");

        boolean first = true;
        while (rs.next()) {
            if (!first) sb.append(',');
            first = false;
            sb.append('[');
            for (int i = 1; i <= n; i++) {
                if (i > 1) sb.append(',');
                String v = rs.getString(i);
                if (v == null) sb.append("null");
                else sb.append(jsonString(v));
            }
            sb.append(']');
        }

        sb.append("]}");
        return sb.toString();
    }

    private static String jsonString(String s) {
        StringBuilder sb = new StringBuilder(s.length() + 2);
        sb.append('"');
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"':  sb.append("\\\""); break;
                case '\\': sb.append("\\\\"); break;
                case '\n': sb.append("\\n");  break;
                case '\r': sb.append("\\r");  break;
                case '\t': sb.append("\\t");  break;
                default:
                    if (c < 0x20) {
                        sb.append(String.format("\\u%04x", (int) c));
                    } else {
                        sb.append(c);
                    }
            }
        }
        sb.append('"');
        return sb.toString();
    }

    private static String envOr(String key, String fallback) {
        String v = System.getenv(key);
        return (v == null || v.isEmpty()) ? fallback : v;
    }
}
