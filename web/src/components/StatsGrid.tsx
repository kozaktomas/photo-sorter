import type { ReactNode } from 'react';

export interface StatItem {
  value: ReactNode;
  label: string;
  color?: 'white' | 'blue' | 'green' | 'yellow' | 'orange' | 'red';
}

interface StatsGridProps {
  items: StatItem[];
  columns?: 2 | 3 | 4;
}

const colorClasses = {
  white: 'text-white',
  blue: 'text-blue-400',
  green: 'text-green-400',
  yellow: 'text-yellow-400',
  orange: 'text-orange-400',
  red: 'text-red-400',
};

const gridColsClasses = {
  2: 'md:grid-cols-2',
  3: 'md:grid-cols-3',
  4: 'md:grid-cols-4',
};

export function StatsGrid({ items, columns = 4 }: StatsGridProps) {
  return (
    <div className={`grid grid-cols-2 ${gridColsClasses[columns]} gap-4`}>
      {items.map((item, index) => (
        <div key={index} className="bg-slate-800 rounded-lg p-4 text-center">
          <div className={`text-2xl font-bold ${colorClasses[item.color || 'white']}`}>
            {item.value}
          </div>
          <div className="text-xs text-slate-400">{item.label}</div>
        </div>
      ))}
    </div>
  );
}

export function StatItem({ value, label, color = 'white' }: StatItem) {
  return (
    <div className="bg-slate-800 rounded-lg p-4 text-center">
      <div className={`text-2xl font-bold ${colorClasses[color]}`}>
        {value}
      </div>
      <div className="text-xs text-slate-400">{label}</div>
    </div>
  );
}
