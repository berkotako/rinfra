// RInfra — Lucide-style line icons (stroke 1.75, 24×24 grid)
// Pixel-identical port of design/project/icons.jsx
import React from "react";

interface IconProps {
  size?: number;
  stroke?: number;
  className?: string;
  style?: React.CSSProperties;
}

type PathDef =
  | string
  | { tag: "circle"; attr: React.SVGProps<SVGCircleElement> }
  | { tag: "line"; attr: React.SVGProps<SVGLineElement> }
  | { tag: "rect"; attr: React.SVGProps<SVGRectElement> }
  | { tag: "polygon"; attr: React.SVGProps<SVGPolygonElement> };

function makeIcon(paths: PathDef[], opts: { fill?: boolean } = {}) {
  const Icon = ({ size = 16, stroke = 1.75, className, style }: IconProps) => (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill={opts.fill ? "currentColor" : "none"}
      stroke={opts.fill ? "none" : "currentColor"}
      strokeWidth={stroke}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      style={style}
    >
      {paths.map((d, i) => {
        if (typeof d === "string") return <path key={i} d={d} />;
        if (d.tag === "circle") return <circle key={i} {...d.attr} />;
        if (d.tag === "line") return <line key={i} {...d.attr} />;
        if (d.tag === "rect") return <rect key={i} {...d.attr} />;
        if (d.tag === "polygon") return <polygon key={i} {...d.attr} />;
        return null;
      })}
    </svg>
  );
  Icon.displayName = "Icon";
  return Icon;
}

// helpers
const c = (cx: number, cy: number, r: number): PathDef => ({ tag: "circle", attr: { cx, cy, r } });
const l = (x1: number, y1: number, x2: number, y2: number): PathDef => ({ tag: "line", attr: { x1, y1, x2, y2 } });
const r = (x: number, y: number, w: number, h: number, rx?: number): PathDef => ({ tag: "rect", attr: { x, y, width: w, height: h, rx } });

export const Dashboard = makeIcon([r(3,3,7,7,1.5), r(14,3,7,7,1.5), r(14,14,7,7,1.5), r(3,14,7,7,1.5)]);
export const Network = makeIcon([c(12,5,2.5), c(5,19,2.5), c(19,19,2.5), "M12 7.5v3M10.5 13l-3.5 3.5M13.5 13l3.5 3.5"]);
export const Target = makeIcon([c(12,12,9), c(12,12,5), c(12,12,1.2)]);
export const FileText = makeIcon(["M14 3v4a1 1 0 0 0 1 1h4", "M17 21H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h7l5 5v11a2 2 0 0 1-2 2z", l(9,13,15,13), l(9,17,13,17)]);
export const Settings = makeIcon([c(12,12,3), "M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"]);
export const Server = makeIcon([r(3,4,18,7,1.5), r(3,13,18,7,1.5), l(7,7.5,7.01,7.5), l(7,16.5,7.01,16.5)]);
export const Globe = makeIcon([c(12,12,9), "M3 12h18", "M12 3a14 14 0 0 1 0 18 14 14 0 0 1 0-18z"]);
export const Radio = makeIcon([c(12,12,2), "M5 12a7 7 0 0 1 7-7 7 7 0 0 1 7 7", "M7.8 14.5a4 4 0 0 1 0-5 4 4 0 0 1 5 0", "M16.2 9.5a4 4 0 0 1 0 5"]);
export const HardDrive = makeIcon([l(22,12,2,12), "M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z", l(6,16,6.01,16), l(10,16,10.01,16)]);
export const Dns = makeIcon([c(12,12,9), "M8 12h8M12 8v8", "M9 9l6 6M15 9l-6 6"]);
export const Plus = makeIcon([l(12,5,12,19), l(5,12,19,12)]);
export const Play = makeIcon([{ tag: "polygon", attr: { points: "6 4 20 12 6 20 6 4" } }]);
export const Check = makeIcon(["M20 6 9 17l-5-5"]);
export const CheckCircle = makeIcon([c(12,12,9), "M9 12l2 2 4-4"]);
export const Trash = makeIcon([l(3,6,21,6), "M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2", l(10,11,10,17), l(14,11,14,17)]);
export const X = makeIcon([l(18,6,6,18), l(6,6,18,18)]);
export const ChevronDown = makeIcon(["M6 9l6 6 6-6"]);
export const ChevronRight = makeIcon(["M9 6l6 6-6 6"]);
export const ChevronLeft = makeIcon(["M15 6l-6 6 6 6"]);
export const Search = makeIcon([c(11,11,8), l(21,21,16.65,16.65)]);
export const Shield = makeIcon(["M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"]);
export const ShieldCheck = makeIcon(["M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z", "M9 12l2 2 4-4"]);
export const Lock = makeIcon([r(3,11,18,11,2), "M7 11V7a5 5 0 0 1 10 0v4"]);
export const User = makeIcon([c(12,8,4), "M4 21a8 8 0 0 1 16 0"]);
export const Activity = makeIcon(["M22 12h-4l-3 9L9 3l-3 9H2"]);
export const Zap = makeIcon(["M13 2 3 14h7l-1 8 10-12h-7l1-8z"]);
export const AlertTriangle = makeIcon(["M10.3 3.3 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.3a2 2 0 0 0-3.4 0z", l(12,9,12,13), l(12,17,12.01,17)]);
export const Info = makeIcon([c(12,12,9), l(12,16,12,12), l(12,8,12.01,8)]);
export const Clock = makeIcon([c(12,12,9), "M12 7v5l3 2"]);
export const Cloud = makeIcon(["M17.5 19a4.5 4.5 0 1 0-1.4-8.8A6 6 0 0 0 4.5 12 4 4 0 0 0 5 19z"]);
export const Layers = makeIcon(["m12 2 9 5-9 5-9-5 9-5z", "m3 12 9 5 9-5", "m3 17 9 5 9-5"]);
export const GitBranch = makeIcon([l(6,3,6,15), c(18,6,3), c(6,18,3), "M18 9a9 9 0 0 1-9 9"]);
export const ArrowRight = makeIcon([l(5,12,19,12), "M13 6l6 6-6 6"]);
export const Dollar = makeIcon([l(12,2,12,22), "M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"]);
export const Bolt = makeIcon(["M13 2 3 14h7l-1 8 10-12h-7l1-8z"]);
export const MapIcon = makeIcon(["M9 4 3 6v14l6-2 6 2 6-2V4l-6 2-6-2z", l(9,4,9,18), l(15,6,15,20)]);
export const Sliders = makeIcon([l(4,21,4,14), l(4,10,4,3), l(12,21,12,12), l(12,8,12,3), l(20,21,20,16), l(20,12,20,3), l(1,14,7,14), l(9,8,15,8), l(17,16,23,16)]);
export const Power = makeIcon(["M18.4 6.6a9 9 0 1 1-12.8 0", l(12,2,12,12)]);
export const Eye = makeIcon(["M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z", c(12,12,3)]);
export const Copy = makeIcon([r(9,9,12,12,2), "M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"]);
export const Link = makeIcon(["M9 12a3 3 0 0 0 3 3h2a4 4 0 0 0 0-8h-1", "M15 12a3 3 0 0 0-3-3h-2a4 4 0 0 0 0 8h1"]);
export const Dot = makeIcon([c(12,12,3)], { fill: true });
export const Filter = makeIcon(["M22 3H2l8 9.5V19l4 2v-8.5L22 3z"]);
export const Refresh = makeIcon(["M21 2v6h-6", "M3 12a9 9 0 0 1 15-6.7L21 8", "M3 22v-6h6", "M21 12a9 9 0 0 1-15 6.7L3 16"]);
export const Building = makeIcon([r(4,2,16,20,2), l(9,7,9,7.01), l(15,7,15,7.01), l(9,11,9,11.01), l(15,11,15,11.01), "M9 22v-4h6v4"]);
export const Calendar = makeIcon([r(3,4,18,18,2), l(16,2,16,6), l(8,2,8,6), l(3,10,21,10)]);
export const Crosshair = makeIcon([c(12,12,9), l(22,12,18,12), l(6,12,2,12), l(12,6,12,2), l(12,22,12,18)]);
export const Terminal = makeIcon(["M4 17l6-6-6-6", l(12,19,20,19)]);
export const Maximize = makeIcon(["M15 3h6v6", "M9 21H3v-6", "M21 3l-7 7", "M3 21l7-7"]);
export const Pause = makeIcon([r(6,4,4,16,1), r(14,4,4,16,1)]);
export const Logo = makeIcon([c(12,12,9), "M12 3a14 14 0 0 1 0 18", "M3.5 9h17M3.5 15h17"]);

// Export all icons as a map for dynamic lookup
export const Icons: Record<string, React.ComponentType<IconProps>> = {
  Dashboard,
  Network,
  Target,
  FileText,
  Settings,
  Server,
  Globe,
  Radio,
  HardDrive,
  Dns,
  Plus,
  Play,
  Check,
  CheckCircle,
  Trash,
  X,
  ChevronDown,
  ChevronRight,
  ChevronLeft,
  Search,
  Shield,
  ShieldCheck,
  Lock,
  User,
  Activity,
  Zap,
  AlertTriangle,
  Info,
  Clock,
  Cloud,
  Layers,
  GitBranch,
  ArrowRight,
  Dollar,
  Bolt,
  Map: MapIcon,
  Sliders,
  Power,
  Eye,
  Copy,
  Link,
  Dot,
  Filter,
  Refresh,
  Building,
  Calendar,
  Crosshair,
  Terminal,
  Maximize,
  Pause,
  Logo,
};

export default Icons;
