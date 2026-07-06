import { Clock3, CreditCard, Gamepad2, UserRound } from "lucide-react";
import type { ReactNode } from "react";
import type { View } from "../types/app";

type MobileNavProps = {
  setView: (view: View) => void;
  view: View;
};

export function MobileNav({ setView, view }: MobileNavProps) {
  const items: Array<{ id: View; icon: ReactNode; label: string }> = [
    { id: "catalog", icon: <Gamepad2 size={20} />, label: "Каталог" },
    { id: "rentals", icon: <Clock3 size={20} />, label: "Аренды" },
    { id: "payments", icon: <CreditCard size={20} />, label: "Платежи" },
    { id: "profile", icon: <UserRound size={20} />, label: "Профиль" }
  ];

  return (
    <nav className="mobile-nav">
      {items.map((item) => (
        <button className={view === item.id ? "active" : ""} key={item.id} onClick={() => setView(item.id)} title={item.label} type="button">
          {item.icon}
        </button>
      ))}
    </nav>
  );
}
