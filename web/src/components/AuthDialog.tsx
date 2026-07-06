import { Sparkles, X } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api, saveTokens, type User } from "../api";
import type { Toast } from "../types/app";

type AuthDialogProps = {
  onAuthenticated: (user: User) => void;
  onClose: () => void;
  setToast: (toast: Toast) => void;
};

export function AuthDialog({ onAuthenticated, onClose, setToast }: AuthDialogProps) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    try {
      if (mode === "login") {
        const tokens = await api.login({ email, password });
        saveTokens(tokens);
      } else {
        const res = await api.register({ email, password, first_name: firstName, last_name: lastName });
        saveTokens({ access_token: res.access_token, refresh_token: res.refresh_token });
      }
      const me = await api.me();
      onAuthenticated(me);
      setToast({ type: "ok", message: "Вы вошли в систему" });
    } catch (error) {
      setToast({ type: "error", message: error instanceof Error ? error.message : "Ошибка авторизации" });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="dialog-backdrop">
      <form className="auth-dialog" onSubmit={submit}>
        <button className="ghost icon-button close-button" onClick={onClose} title="Закрыть" type="button">
          <X size={20} />
        </button>
        <Sparkles size={28} />
        <h2>{mode === "login" ? "Вход" : "Регистрация"}</h2>
        <label>
          Email
          <input onChange={(event) => setEmail(event.target.value)} required type="email" value={email} />
        </label>
        <label>
          Пароль
          <input onChange={(event) => setPassword(event.target.value)} required type="password" value={password} />
        </label>
        {mode === "register" && (
          <div className="two-fields">
            <label>
              Имя
              <input onChange={(event) => setFirstName(event.target.value)} required value={firstName} />
            </label>
            <label>
              Фамилия
              <input onChange={(event) => setLastName(event.target.value)} required value={lastName} />
            </label>
          </div>
        )}
        <button className="primary-button full" disabled={busy} type="submit">
          {busy ? "Проверка..." : mode === "login" ? "Войти" : "Создать аккаунт"}
        </button>
        <button className="ghost switch-auth" onClick={() => setMode(mode === "login" ? "register" : "login")} type="button">
          {mode === "login" ? "Нужна регистрация" : "Уже есть аккаунт"}
        </button>
      </form>
    </div>
  );
}
