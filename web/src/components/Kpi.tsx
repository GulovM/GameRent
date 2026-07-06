import type { ReactNode } from "react";

type KpiProps = {
  icon: ReactNode;
  label: string;
  value: string | number;
};

export function Kpi({ icon, label, value }: KpiProps) {
  return (
    <article className="kpi-card">
      {icon}
      <span>{label}</span>
      <strong>{value}</strong>
    </article>
  );
}
