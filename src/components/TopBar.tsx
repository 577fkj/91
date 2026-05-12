import { Globe } from "lucide-react";

export function TopBar() {
  return (
    <div className="top-bar">
      <div className="container top-bar__inner">
        <div className="top-bar__side">
          <a href="#lang" aria-label="切换语言">
            <Globe size={12} style={{ marginRight: 4, verticalAlign: -1 }} />
            简体中文
          </a>
        </div>
      </div>
    </div>
  );
}
