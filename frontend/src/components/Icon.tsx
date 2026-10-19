// Inline SVG icons (16×16 strokes; the page loads nothing from the network).

const PATHS: Record<string, string> = {
  search: 'M7 2.5a4.5 4.5 0 1 1 0 9 4.5 4.5 0 0 1 0-9zM10.5 10.5 14 14',
  sun: 'M8 5a3 3 0 1 1 0 6 3 3 0 0 1 0-6zM8 1v1.5M8 13.5V15M1 8h1.5M13.5 8H15M3 3l1 1M12 12l1 1M3 13l1-1M12 4l1-1',
  moon: 'M13.5 9.5A5.5 5.5 0 0 1 6.5 2.5a5.5 5.5 0 1 0 7 7z',
  help: 'M8 1.5a6.5 6.5 0 1 1 0 13 6.5 6.5 0 0 1 0-13zM6.2 6.2a1.9 1.9 0 1 1 2.6 1.8c-.5.2-.8.6-.8 1.1v.4M8 11.5v.2',
  copy: 'M5.5 5.5h7v8h-7zM3.5 10.5v-8h7',
  check: 'M3 8.5l3 3 7-7',
  x: 'M4 4l8 8M12 4l-8 8',
  chevronRight: 'M6 3.5 10.5 8 6 12.5',
  chevronDown: 'M3.5 6 8 10.5 12.5 6',
  chevronLeft: 'M10 3.5 5.5 8 10 12.5',
  external: 'M9 2.5h4.5V7M13.5 2.5 7.5 8.5M11.5 9.5v4h-9v-9h4',
  file: 'M4 1.5h5l3 3v10H4zM9 1.5v3h3',
  folder: 'M1.5 3.5h4.5l1.5 1.5h7v8.5h-13z',
  refresh: 'M13.5 8a5.5 5.5 0 1 1-1.6-3.9M13.5 2.5V5h-2.5',
  download: 'M8 2v8.5M4.5 7 8 10.5 11.5 7M2.5 13.5h11',
  filter: 'M2 3h12L9.5 8.5v4.5l-3 1.5v-6z',
  warning: 'M8 2 14.5 13.5h-13zM8 6.5v3M8 11.5v.2',
  info: 'M8 1.5a6.5 6.5 0 1 1 0 13 6.5 6.5 0 0 1 0-13zM8 7.5v4M8 5v.2',
  error: 'M8 1.5a6.5 6.5 0 1 1 0 13 6.5 6.5 0 0 1 0-13zM5.5 5.5l5 5M10.5 5.5l-5 5',
  play: 'M5 3.5v9l7-4.5z',
  code: 'M5.5 4.5 2 8l3.5 3.5M10.5 4.5 14 8l-3.5 3.5',
  graph: 'M4 2a2 2 0 1 1 0 4 2 2 0 0 1 0-4zM12 6a2 2 0 1 1 0 4 2 2 0 0 1 0-4zM4 10a2 2 0 1 1 0 4 2 2 0 0 1 0-4zM4 6v4M6 4.6l4 2.4M6 11.4l4-2.4',
  shield: 'M8 1.5 13.5 3.5v4c0 3.5-2.4 6-5.5 7-3.1-1-5.5-3.5-5.5-7v-4z',
  globe: 'M8 1.5a6.5 6.5 0 1 1 0 13 6.5 6.5 0 0 1 0-13zM1.5 8h13M8 1.5c2 2 2.8 4.2 2.8 6.5S10 12.5 8 14.5C6 12.5 5.2 10.3 5.2 8S6 3.5 8 1.5z',
  diff: 'M5 2v7M1.5 5.5h7M2 12.5h6M11 2.5v11M11 13.5l-2-2M11 13.5l2-2',
  beaker: 'M6 1.5h4M6.5 1.5v4.5L2.5 13a1 1 0 0 0 .9 1.5h9.2a1 1 0 0 0 .9-1.5L9.5 6V1.5M4.5 10h7',
  flow: 'M2 3.5h4v3H2zM10 9.5h4v3h-4zM6 5h2.5v6H10',
  microscope: 'M7 1.5l2.5 1.5-3 5-2.5-1.5zM6 8.5c1 1.5 2.7 2 4.5 1.5M3 14.5h10M8 14.5v-2M10.5 10A3.5 3.5 0 0 0 9 4.5',
  book: 'M2.5 2.5h4c1 0 1.5.5 1.5 1.5v10c0-1-.5-1.5-1.5-1.5h-4zM13.5 2.5h-4c-1 0-1.5.5-1.5 1.5v10c0-1 .5-1.5 1.5-1.5h4z',
  gauge: 'M2.5 11.5a5.5 5.5 0 1 1 11 0M8 11.5 11 6.5',
  menu: 'M2.5 4h11M2.5 8h11M2.5 12h11',
  panelLeft: 'M2 2.5h12v11H2zM6 2.5v11',
  panelRight: 'M2 2.5h12v11H2zM10 2.5v11',
  dot: 'M8 6a2 2 0 1 1 0 4 2 2 0 0 1 0-4z',
  plus: 'M8 3v10M3 8h10',
  minus: 'M3 8h10',
  target: 'M8 1.5a6.5 6.5 0 1 1 0 13 6.5 6.5 0 0 1 0-13zM8 5a3 3 0 1 1 0 6 3 3 0 0 1 0-6zM8 7.5v1',
  sparkles: 'M6 2l1 3 3 1-3 1-1 3-1-3-3-1 3-1zM12 9l.6 1.6 1.4.4-1.4.6L12 13l-.6-1.4-1.4-.6 1.4-.4z',
  print: 'M4.5 5.5v-4h7v4M4.5 11.5h-2v-6h11v6h-2M4.5 9.5h7v5h-7z',
}

export type IconName = keyof typeof PATHS

export function Icon({ name, className, title }: { name: IconName; className?: string; title?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      className={'inline-block shrink-0 ' + (className ?? 'size-4')}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden={title ? undefined : true}
      role={title ? 'img' : undefined}
    >
      {title && <title>{title}</title>}
      <path d={PATHS[name]} />
    </svg>
  )
}

export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" className={className ?? 'size-5'} aria-hidden="true">
      <path d="M4 6.2v3.6M6 4.6l4 2.4M6 11.4l4-2.4" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" />
      <circle cx="4" cy="4" r="2.2" fill="currentColor" />
      <circle cx="12" cy="8" r="2.2" fill="currentColor" />
      <circle cx="4" cy="12" r="2.2" fill="currentColor" />
    </svg>
  )
}
