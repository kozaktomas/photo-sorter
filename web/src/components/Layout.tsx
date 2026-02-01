import { useState, useRef, useEffect } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { FolderOpen, Tags, Sparkles, Home, Users, Images, Camera, Maximize2, AlertTriangle, Type, ShieldCheck, Cpu, ChevronDown, LogOut } from 'lucide-react';
import { useAuth } from '../hooks/useAuth';
import { LanguageSwitcher } from './LanguageSwitcher';

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

function NavDropdown({ group, isActive }: { group: NavGroup; isActive: (path: string) => boolean }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  const hasActiveChild = group.items.some(item => isActive(item.path));

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
          hasActiveChild
            ? 'bg-slate-700 text-white'
            : 'text-slate-300 hover:bg-slate-700 hover:text-white'
        }`}
      >
        <span>{group.label}</span>
        <ChevronDown className={`h-3.5 w-3.5 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div className="absolute top-full left-0 mt-1 w-44 bg-slate-800 border border-slate-700 rounded-md shadow-lg py-1 z-50">
          {group.items.map(({ path, icon: Icon, label }) => (
            <Link
              key={path}
              to={path}
              onClick={() => setOpen(false)}
              className={`flex items-center space-x-2 px-3 py-2 text-sm transition-colors ${
                isActive(path)
                  ? 'bg-slate-700 text-white'
                  : 'text-slate-300 hover:bg-slate-700 hover:text-white'
              }`}
            >
              <Icon className="h-4 w-4" />
              <span>{label}</span>
            </Link>
          ))}
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
              {primaryItems.map(({ path, icon: Icon, label }) => (
                <Link
                  key={path}
                  to={path}
                  className={`flex items-center space-x-2 px-3 py-2 rounded-md text-sm font-medium transition-colors ${
                    isActive(path)
                      ? 'bg-slate-700 text-white'
                      : 'text-slate-300 hover:bg-slate-700 hover:text-white'
                  }`}
                >
                  <Icon className="h-4 w-4" />
                  <span>{label}</span>
                </Link>
              ))}

              <div className="w-px h-6 bg-slate-600 mx-2" />

              {groups.map(group => (
                <NavDropdown key={group.label} group={group} isActive={isActive} />
              ))}
            </nav>

            <div className="flex items-center space-x-2">
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
