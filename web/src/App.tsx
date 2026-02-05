import { BrowserRouter, Routes, Route, Navigate, useLocation } from 'react-router-dom';
import { ErrorBoundary } from './components/ErrorBoundary';
import { Layout } from './components/Layout';
import { DashboardPage } from './pages/Dashboard';
import { AlbumsPage } from './pages/Albums';
import { PhotosPage } from './pages/Photos/index';
import { PhotoDetailPage } from './pages/PhotoDetail';
import { LabelsPage } from './pages/Labels';
import { AnalyzePage } from './pages/Analyze/index';
import { FacesPage } from './pages/Faces/index';
import { SimilarPhotosPage } from './pages/SimilarPhotos';
import { ExpandPage } from './pages/Expand';
import { OutliersPage } from './pages/Outliers';
import { TextSearchPage } from './pages/TextSearch';
import { RecognitionPage } from './pages/Recognition/index';
import { ProcessPage } from './pages/Process';
import { DuplicatesPage } from './pages/Duplicates/index';
import { SuggestAlbumsPage } from './pages/SuggestAlbums/index';
import { LabelDetailPage } from './pages/LabelDetail';
import { SubjectDetailPage } from './pages/SubjectDetail';
import { LoginPage } from './pages/Login';
import { AuthProvider, useAuth } from './hooks/useAuth';

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, isLoading } = useAuth();
  const location = useLocation();

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-slate-900">
        <div className="text-slate-400">Loading...</div>
      </div>
    );
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return <>{children}</>;
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        path="/"
        element={
          <ProtectedRoute>
            <Layout>
              <DashboardPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/albums"
        element={
          <ProtectedRoute>
            <Layout>
              <AlbumsPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/albums/:uid"
        element={
          <ProtectedRoute>
            <Layout>
              <AlbumsPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/photos"
        element={
          <ProtectedRoute>
            <Layout>
              <PhotosPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/photos/:uid"
        element={
          <ProtectedRoute>
            <Layout>
              <PhotoDetailPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/labels"
        element={
          <ProtectedRoute>
            <Layout>
              <LabelsPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/labels/:uid"
        element={
          <ProtectedRoute>
            <Layout>
              <LabelDetailPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/subjects/:uid"
        element={
          <ProtectedRoute>
            <Layout>
              <SubjectDetailPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/analyze"
        element={
          <ProtectedRoute>
            <Layout>
              <AnalyzePage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/faces"
        element={
          <ProtectedRoute>
            <Layout>
              <FacesPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/similar"
        element={
          <ProtectedRoute>
            <Layout>
              <SimilarPhotosPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/expand"
        element={
          <ProtectedRoute>
            <Layout>
              <ExpandPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/outliers"
        element={
          <ProtectedRoute>
            <Layout>
              <OutliersPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/recognition"
        element={
          <ProtectedRoute>
            <Layout>
              <RecognitionPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/process"
        element={
          <ProtectedRoute>
            <Layout>
              <ProcessPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/text-search"
        element={
          <ProtectedRoute>
            <Layout>
              <TextSearchPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/duplicates"
        element={
          <ProtectedRoute>
            <Layout>
              <DuplicatesPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/suggest-albums"
        element={
          <ProtectedRoute>
            <Layout>
              <SuggestAlbumsPage />
            </Layout>
          </ProtectedRoute>
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

export default function App() {
  return (
    <ErrorBoundary>
      <BrowserRouter>
        <AuthProvider>
          <AppRoutes />
        </AuthProvider>
      </BrowserRouter>
    </ErrorBoundary>
  );
}
