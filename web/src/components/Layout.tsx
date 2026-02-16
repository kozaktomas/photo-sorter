import { useState, useRef, useEffect } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { FolderOpen, Tags, Sparkles, Home, Users, Images, Camera, Maximize2, AlertTriangle, Type, ShieldCheck, Cpu, ChevronDown, LogOut, Copy, FolderSearch, BookOpen } from 'lucide-react';
import { useAuth } from '../hooks/useAuth';
import { LanguageSwitcher } from './LanguageSwitcher';
import { getPageConfigForPath, colorMap } from '../constants/pageConfig';
import type { ColorClasses } from '../constants/pageConfig';

interface LayoutProps {
  children: React.ReactNode;
}

interface NavItem {
  path: string;
  icon: React.ComponentType<{ className?: string }>;
  label: string;
}

interface NavGroup {
  label: string;
  items: NavItem[];
}

function getColorForPath(pathname: string): ColorClasses | null {
  const config = getPageConfigForPath(pathname);
  return config ? colorMap[config.color] : null;
}

function NavDropdown({ group, isActive }: { group: NavGroup; isActive: (path: string) => boolean }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  const hasActiveChild = group.items.some(item => isActive(item.path));
  // Get the active child's color for the group button
  const activeChild = group.items.find(item => isActive(item.path));
  const activeColor = activeChild ? getColorForPath(activeChild.path) : null;

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, []);

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen(!open)}
        className={`flex items-center space-x-1.5 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
          hasActiveChild && activeColor
            ? `${activeColor.navActiveBg} ${activeColor.navActive}`
            : 'text-slate-300 hover:bg-slate-700 hover:text-white'
        }`}
      >
        <span>{group.label}</span>
        <ChevronDown className={`h-3.5 w-3.5 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div className="absolute top-full left-0 mt-1 w-44 bg-slate-800 border border-slate-700 rounded-md shadow-lg py-1 z-50">
          {group.items.map(({ path, icon: Icon, label }) => {
            const itemActive = isActive(path);
            const itemColor = itemActive ? getColorForPath(path) : null;
            return (
              <Link
                key={path}
                to={path}
                onClick={() => setOpen(false)}
                className={`flex items-center space-x-2 px-3 py-2 text-sm transition-colors ${
                  itemActive && itemColor
                    ? `${itemColor.navActiveBg} ${itemColor.navActive}`
                    : 'text-slate-300 hover:bg-slate-700 hover:text-white'
                }`}
              >
                {itemActive && itemColor && (
                  <span className={`w-1 h-1 rounded-full ${itemColor.buttonBg} shrink-0`} />
                )}
                <Icon className="h-4 w-4" />
                <span>{label}</span>
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}

export function Layout({ children }: LayoutProps) {
  const location = useLocation();
  const { logout } = useAuth();
  const { t } = useTranslation('common');

  const handleLogout = async () => {
    await logout();
  };

  const primaryItems: NavItem[] = [
    { path: '/', icon: Home, label: t('nav.dashboard') },
    { path: '/albums', icon: FolderOpen, label: t('nav.albums') },
    { path: '/photos', icon: Camera, label: t('nav.photos') },
    { path: '/labels', icon: Tags, label: t('nav.labels') },
  ];

  const groups: NavGroup[] = [
    {
      label: t('nav.ai'),
      items: [
        { path: '/analyze', icon: Sparkles, label: t('nav.analyze') },
        { path: '/text-search', icon: Type, label: t('nav.textSearch') },
      ],
    },
    {
      label: t('nav.faces'),
      items: [
        { path: '/faces', icon: Users, label: t('nav.faces') },
        { path: '/recognition', icon: ShieldCheck, label: t('nav.recognition') },
        { path: '/outliers', icon: AlertTriangle, label: t('nav.outliers') },
      ],
    },
    {
      label: t('nav.tools'),
      items: [
        { path: '/similar', icon: Images, label: t('nav.similar') },
        { path: '/expand', icon: Maximize2, label: t('nav.expand') },
        { path: '/duplicates', icon: Copy, label: t('nav.duplicates') },
        { path: '/suggest-albums', icon: FolderSearch, label: t('nav.suggestAlbums') },
        { path: '/books', icon: BookOpen, label: t('nav.books') },
        { path: '/process', icon: Cpu, label: t('nav.process') },
      ],
    },
  ];

  const isActive = (path: string) => {
    if (path === '/') return location.pathname === '/';
    return location.pathname.startsWith(path);
  };

  return (
    <div className="min-h-screen flex flex-col">
      <header className="bg-slate-800 border-b border-slate-700">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex items-center justify-between h-16">
            <nav className="flex items-center space-x-1">
              {primaryItems.map(({ path, icon: Icon, label }) => {
                const active = isActive(path);
                const c = active ? getColorForPath(path) : null;
                return (
                  <Link
                    key={path}
                    to={path}
                    className={`flex items-center space-x-2 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                      active && c
                        ? `${c.navActiveBg} ${c.navActive}`
                        : 'text-slate-300 hover:bg-slate-700 hover:text-white'
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                    <span>{label}</span>
                  </Link>
                );
              })}

              <div className="w-px h-6 bg-slate-600 mx-2" />

              {groups.map(group => (
                <NavDropdown key={group.label} group={group} isActive={isActive} />
              ))}
            </nav>

            <div className="flex items-center space-x-2">
              <a
                href="https://github.com/kozaktomas/photo-sorter"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center px-2 py-2 rounded-md text-slate-300 hover:bg-slate-700 hover:text-white transition-colors"
                title="GitHub"
              >
                <svg className="h-5 w-5" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/></svg>
              </a>
              <LanguageSwitcher />
              <button
                onClick={handleLogout}
                className="flex items-center space-x-2 px-3 py-2 rounded-md text-sm font-medium text-slate-300 hover:bg-slate-700 hover:text-white transition-colors"
                title={t('nav.signOut')}
              >
                <LogOut className="h-4 w-4" />
                <span>{t('nav.signOut')}</span>
              </button>
            </div>
          </div>
        </div>
      </header>

      <main className="flex-1 bg-slate-900">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
          {children}
        </div>
      </main>
    </div>
  );
}
