import type { ComponentType } from 'react';
import {
  Home, FolderOpen, Camera, Tags, Sparkles, Type,
  Users, ShieldCheck, AlertTriangle, Images, Maximize2,
  Copy, FolderSearch, Cpu, Play, BookOpen,
} from 'lucide-react';

export type AccentColor =
  | 'sky' | 'blue' | 'indigo' | 'cyan'
  | 'violet' | 'purple'
  | 'amber' | 'yellow' | 'orange'
  | 'teal' | 'emerald' | 'lime' | 'green' | 'rose';

export type Category = 'browse' | 'ai' | 'faces' | 'tools';

export interface PageConfig {
  color: AccentColor;
  icon: ComponentType<{ className?: string }>;
  category?: Category;
}

export const PAGE_CONFIGS: Record<string, PageConfig> = {
  dashboard:      { color: 'sky',     icon: Home,           category: 'browse' },
  albums:         { color: 'blue',    icon: FolderOpen,     category: 'browse' },
  photos:         { color: 'indigo',  icon: Camera,         category: 'browse' },
  labels:         { color: 'cyan',    icon: Tags,           category: 'browse' },
  analyze:        { color: 'violet',  icon: Sparkles,       category: 'ai' },
  textSearch:     { color: 'purple',  icon: Type,           category: 'ai' },
  faces:          { color: 'amber',   icon: Users,          category: 'faces' },
  recognition:    { color: 'yellow',  icon: ShieldCheck,    category: 'faces' },
  outliers:       { color: 'orange',  icon: AlertTriangle,  category: 'faces' },
  similar:        { color: 'teal',    icon: Images,         category: 'tools' },
  expand:         { color: 'emerald', icon: Maximize2,      category: 'tools' },
  duplicates:     { color: 'lime',    icon: Copy,           category: 'tools' },
  suggestAlbums:  { color: 'green',   icon: FolderSearch,   category: 'tools' },
  process:        { color: 'rose',    icon: Cpu,            category: 'tools' },
  books:          { color: 'rose',    icon: BookOpen,       category: 'tools' },
  // Detail pages inherit parent colors
  photoDetail:    { color: 'indigo',  icon: Camera },
  labelDetail:    { color: 'cyan',    icon: Tags },
  subjectDetail:  { color: 'amber',   icon: Users },
  compare:        { color: 'lime',    icon: Copy },
  slideshow:      { color: 'indigo',  icon: Play },
  bookEditor:     { color: 'rose',    icon: BookOpen },
};

export interface ColorClasses {
  gradient: string;
  iconBg: string;
  iconText: string;
  badgeBg: string;
  badgeText: string;
  badgeBorder: string;
  topBorder: string;
  navActive: string;
  navActiveBg: string;
  buttonBg: string;
  buttonHover: string;
}

// Static mapping — full class strings to survive Tailwind's static analysis / purge
export const colorMap: Record<AccentColor, ColorClasses> = {
  sky: {
    gradient: 'bg-gradient-to-r from-sky-500 via-sky-400 to-sky-600',
    iconBg: 'bg-sky-500/15',
    iconText: 'text-sky-400',
    badgeBg: 'bg-sky-500/10',
    badgeText: 'text-sky-400',
    badgeBorder: 'border-sky-500/20',
    topBorder: 'border-t-sky-500',
    navActive: 'text-sky-400',
    navActiveBg: 'bg-sky-500/10',
    buttonBg: 'bg-sky-600',
    buttonHover: 'hover:bg-sky-700',
  },
  blue: {
    gradient: 'bg-gradient-to-r from-blue-500 via-blue-400 to-blue-600',
    iconBg: 'bg-blue-500/15',
    iconText: 'text-blue-400',
    badgeBg: 'bg-blue-500/10',
    badgeText: 'text-blue-400',
    badgeBorder: 'border-blue-500/20',
    topBorder: 'border-t-blue-500',
    navActive: 'text-blue-400',
    navActiveBg: 'bg-blue-500/10',
    buttonBg: 'bg-blue-600',
    buttonHover: 'hover:bg-blue-700',
  },
  indigo: {
    gradient: 'bg-gradient-to-r from-indigo-500 via-indigo-400 to-indigo-600',
    iconBg: 'bg-indigo-500/15',
    iconText: 'text-indigo-400',
    badgeBg: 'bg-indigo-500/10',
    badgeText: 'text-indigo-400',
    badgeBorder: 'border-indigo-500/20',
    topBorder: 'border-t-indigo-500',
    navActive: 'text-indigo-400',
    navActiveBg: 'bg-indigo-500/10',
    buttonBg: 'bg-indigo-600',
    buttonHover: 'hover:bg-indigo-700',
  },
  cyan: {
    gradient: 'bg-gradient-to-r from-cyan-500 via-cyan-400 to-cyan-600',
    iconBg: 'bg-cyan-500/15',
    iconText: 'text-cyan-400',
    badgeBg: 'bg-cyan-500/10',
    badgeText: 'text-cyan-400',
    badgeBorder: 'border-cyan-500/20',
    topBorder: 'border-t-cyan-500',
    navActive: 'text-cyan-400',
    navActiveBg: 'bg-cyan-500/10',
    buttonBg: 'bg-cyan-600',
    buttonHover: 'hover:bg-cyan-700',
  },
  violet: {
    gradient: 'bg-gradient-to-r from-violet-500 via-violet-400 to-violet-600',
    iconBg: 'bg-violet-500/15',
    iconText: 'text-violet-400',
    badgeBg: 'bg-violet-500/10',
    badgeText: 'text-violet-400',
    badgeBorder: 'border-violet-500/20',
    topBorder: 'border-t-violet-500',
    navActive: 'text-violet-400',
    navActiveBg: 'bg-violet-500/10',
    buttonBg: 'bg-violet-600',
    buttonHover: 'hover:bg-violet-700',
  },
  purple: {
    gradient: 'bg-gradient-to-r from-purple-500 via-purple-400 to-purple-600',
    iconBg: 'bg-purple-500/15',
    iconText: 'text-purple-400',
    badgeBg: 'bg-purple-500/10',
    badgeText: 'text-purple-400',
    badgeBorder: 'border-purple-500/20',
    topBorder: 'border-t-purple-500',
    navActive: 'text-purple-400',
    navActiveBg: 'bg-purple-500/10',
    buttonBg: 'bg-purple-600',
    buttonHover: 'hover:bg-purple-700',
  },
  amber: {
    gradient: 'bg-gradient-to-r from-amber-500 via-amber-400 to-amber-600',
    iconBg: 'bg-amber-500/15',
    iconText: 'text-amber-400',
    badgeBg: 'bg-amber-500/10',
    badgeText: 'text-amber-400',
    badgeBorder: 'border-amber-500/20',
    topBorder: 'border-t-amber-500',
    navActive: 'text-amber-400',
    navActiveBg: 'bg-amber-500/10',
    buttonBg: 'bg-amber-600',
    buttonHover: 'hover:bg-amber-700',
  },
  yellow: {
    gradient: 'bg-gradient-to-r from-yellow-500 via-yellow-400 to-yellow-600',
    iconBg: 'bg-yellow-500/15',
    iconText: 'text-yellow-400',
    badgeBg: 'bg-yellow-500/10',
    badgeText: 'text-yellow-400',
    badgeBorder: 'border-yellow-500/20',
    topBorder: 'border-t-yellow-500',
    navActive: 'text-yellow-400',
    navActiveBg: 'bg-yellow-500/10',
    buttonBg: 'bg-yellow-600',
    buttonHover: 'hover:bg-yellow-700',
  },
  orange: {
    gradient: 'bg-gradient-to-r from-orange-500 via-orange-400 to-orange-600',
    iconBg: 'bg-orange-500/15',
    iconText: 'text-orange-400',
    badgeBg: 'bg-orange-500/10',
    badgeText: 'text-orange-400',
    badgeBorder: 'border-orange-500/20',
    topBorder: 'border-t-orange-500',
    navActive: 'text-orange-400',
    navActiveBg: 'bg-orange-500/10',
    buttonBg: 'bg-orange-600',
    buttonHover: 'hover:bg-orange-700',
  },
  teal: {
    gradient: 'bg-gradient-to-r from-teal-500 via-teal-400 to-teal-600',
    iconBg: 'bg-teal-500/15',
    iconText: 'text-teal-400',
    badgeBg: 'bg-teal-500/10',
    badgeText: 'text-teal-400',
    badgeBorder: 'border-teal-500/20',
    topBorder: 'border-t-teal-500',
    navActive: 'text-teal-400',
    navActiveBg: 'bg-teal-500/10',
    buttonBg: 'bg-teal-600',
    buttonHover: 'hover:bg-teal-700',
  },
  emerald: {
    gradient: 'bg-gradient-to-r from-emerald-500 via-emerald-400 to-emerald-600',
    iconBg: 'bg-emerald-500/15',
    iconText: 'text-emerald-400',
    badgeBg: 'bg-emerald-500/10',
    badgeText: 'text-emerald-400',
    badgeBorder: 'border-emerald-500/20',
    topBorder: 'border-t-emerald-500',
    navActive: 'text-emerald-400',
    navActiveBg: 'bg-emerald-500/10',
    buttonBg: 'bg-emerald-600',
    buttonHover: 'hover:bg-emerald-700',
  },
  lime: {
    gradient: 'bg-gradient-to-r from-lime-500 via-lime-400 to-lime-600',
    iconBg: 'bg-lime-500/15',
    iconText: 'text-lime-400',
    badgeBg: 'bg-lime-500/10',
    badgeText: 'text-lime-400',
    badgeBorder: 'border-lime-500/20',
    topBorder: 'border-t-lime-500',
    navActive: 'text-lime-400',
    navActiveBg: 'bg-lime-500/10',
    buttonBg: 'bg-lime-600',
    buttonHover: 'hover:bg-lime-700',
  },
  green: {
    gradient: 'bg-gradient-to-r from-green-500 via-green-400 to-green-600',
    iconBg: 'bg-green-500/15',
    iconText: 'text-green-400',
    badgeBg: 'bg-green-500/10',
    badgeText: 'text-green-400',
    badgeBorder: 'border-green-500/20',
    topBorder: 'border-t-green-500',
    navActive: 'text-green-400',
    navActiveBg: 'bg-green-500/10',
    buttonBg: 'bg-green-600',
    buttonHover: 'hover:bg-green-700',
  },
  rose: {
    gradient: 'bg-gradient-to-r from-rose-500 via-rose-400 to-rose-600',
    iconBg: 'bg-rose-500/15',
    iconText: 'text-rose-400',
    badgeBg: 'bg-rose-500/10',
    badgeText: 'text-rose-400',
    badgeBorder: 'border-rose-500/20',
    topBorder: 'border-t-rose-500',
    navActive: 'text-rose-400',
    navActiveBg: 'bg-rose-500/10',
    buttonBg: 'bg-rose-600',
    buttonHover: 'hover:bg-rose-700',
  },
};

// Path → page config lookup for Layout navigation
export const PATH_TO_PAGE: Record<string, string> = {
  '/': 'dashboard',
  '/albums': 'albums',
  '/photos': 'photos',
  '/labels': 'labels',
  '/analyze': 'analyze',
  '/text-search': 'textSearch',
  '/faces': 'faces',
  '/recognition': 'recognition',
  '/outliers': 'outliers',
  '/similar': 'similar',
  '/expand': 'expand',
  '/duplicates': 'duplicates',
  '/suggest-albums': 'suggestAlbums',
  '/process': 'process',
  '/books': 'books',
};

export function getPageConfigForPath(pathname: string): PageConfig | undefined {
  // Exact match first
  const exact = PATH_TO_PAGE[pathname];
  if (exact) return PAGE_CONFIGS[exact];

  // Prefix match for detail pages
  if (pathname.startsWith('/photos/')) return PAGE_CONFIGS.photoDetail;
  if (pathname.startsWith('/labels/')) return PAGE_CONFIGS.labelDetail;
  if (pathname.startsWith('/subjects/')) return PAGE_CONFIGS.subjectDetail;
  if (pathname.startsWith('/compare')) return PAGE_CONFIGS.compare;
  if (pathname.startsWith('/albums/')) return PAGE_CONFIGS.albums;
  if (pathname.startsWith('/slideshow')) return PAGE_CONFIGS.slideshow;
  if (pathname.startsWith('/books/')) return PAGE_CONFIGS.bookEditor;

  return undefined;
}
