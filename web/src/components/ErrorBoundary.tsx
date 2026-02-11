import { Component, type ErrorInfo, type ReactNode } from 'react';
import { AlertTriangle, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from './Button';
import { Card, CardContent } from './Card';

// eslint-disable-next-line react-refresh/only-export-components
function ErrorFallbackUI({ error, onReset, onReload }: {
  error: Error | null;
  onReset: () => void;
  onReload: () => void;
}) {
  const { t } = useTranslation('common');

  return (
    <div className="min-h-[400px] flex items-center justify-center p-8">
      <Card className="max-w-lg w-full">
        <CardContent>
          <div className="text-center space-y-4">
            <div className="flex justify-center">
              <div className="p-3 bg-red-500/10 rounded-full">
                <AlertTriangle className="h-8 w-8 text-red-400" />
              </div>
            </div>

            <div>
              <h2 className="text-xl font-semibold text-white mb-2">
                {t('errors.somethingWentWrong')}
              </h2>
              <p className="text-slate-400 text-sm">
                {t('errors.unexpectedError')}
              </p>
            </div>

            {error && (
              <div className="bg-slate-800 rounded-lg p-3 text-left">
                <p className="text-sm text-red-400 font-mono break-all">
                  {error.message}
                </p>
              </div>
            )}

            <div className="flex gap-3 justify-center pt-2">
              <Button variant="secondary" onClick={onReset}>
                {t('buttons.tryAgain')}
              </Button>
              <Button onClick={onReload}>
                <RefreshCw className="h-4 w-4 mr-2" />
                {t('buttons.reloadPage')}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
  errorInfo: ErrorInfo | null;
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false, error: null, errorInfo: null };
  }

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('ErrorBoundary caught an error:', error, errorInfo);
    this.setState({ errorInfo });
  }

  handleReload = () => {
    window.location.reload();
  };

  handleReset = () => {
    this.setState({ hasError: false, error: null, errorInfo: null });
  };

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }

      return (
        <ErrorFallbackUI
          error={this.state.error}
          onReset={this.handleReset}
          onReload={this.handleReload}
        />
      );
    }

    return this.props.children;
  }
}
