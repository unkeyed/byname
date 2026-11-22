import { useState } from "react";
import { logoURL } from "./api";
import { useLogos } from "./queries";
import { cx } from "./ui";

/** One of the eight series hues, the same for a name every time. */
function hue(name: string): number {
  let h = 0;
  for (const c of name.toLowerCase()) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return (h % 8) + 1;
}

/**
 * A brand's logo in a fixed square, drawn over its monogram: the first
 * letter in a colour taken from the name. The monogram shows until a logo
 * is stored and has loaded, and stays if it fails; the logo then covers it
 * in the same box, so nothing moves.
 */
export function BrandMark({ id, name, size = 20 }: { id: number; name: string; size?: number }) {
  const logos = useLogos();
  const logo = logos.data?.get(id);
  const src = logo && logoURL(logo);
  const [failed, setFailed] = useState<string>();
  const [loaded, setLoaded] = useState<string>();
  const color = `var(--series-${hue(name)})`;
  const letter = name.match(/[\p{L}\p{N}]/u)?.[0]?.toUpperCase() ?? "?";
  return (
    <span
      aria-hidden="true"
      className="relative inline-grid shrink-0 place-items-center overflow-hidden rounded-[5px] font-semibold select-none"
      style={{ width: size, height: size, fontSize: Math.round(size * 0.55), color, background: `color-mix(in oklab, ${color} 16%, var(--surface))` }}
    >
      {letter}
      {src && src !== failed && (
        <img
          src={src}
          alt=""
          width={size}
          height={size}
          onLoad={() => setLoaded(src)}
          onError={() => setFailed(src)}
          className={cx("absolute inset-0 size-full bg-white object-contain ring-1 ring-line ring-inset transition-opacity", loaded === src ? "opacity-100" : "opacity-0")}
        />
      )}
    </span>
  );
}
