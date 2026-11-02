import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/sphere.css'
import './styles/app.css'
import './styles/settings.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import Home from './pages/Home'
import Scenes from './pages/Scenes'
import ScenePage from './pages/ScenePage'
import Performers, { PerformerPage } from './pages/Performers'
import { GroupPage, Groups, StudioPage, Studios, TagPage, Tags } from './pages/Catalog'
import Galleries, { GalleryPage } from './pages/Galleries'
import Markers from './pages/Markers'
import Downloads from './pages/Downloads'
import Stats, { Duplicates } from './pages/Stats'
import Settings from './pages/Settings'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  return <>{children}</>
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><Settings /></ErrorBoundary></Gate>} />
      <Route element={<Gate><ErrorBoundary where="sphere"><Layout /></ErrorBoundary></Gate>}>
        <Route path="/" element={<Home />} />
        <Route path="/scenes" element={<Scenes />} />
        <Route path="/scenes/:id" element={<ScenePage />} />
        <Route path="/performers" element={<Performers />} />
        <Route path="/performers/:id" element={<PerformerPage />} />
        <Route path="/studios" element={<Studios />} />
        <Route path="/studios/:id" element={<StudioPage />} />
        <Route path="/tags" element={<Tags />} />
        <Route path="/tags/:id" element={<TagPage />} />
        <Route path="/groups" element={<Groups />} />
        <Route path="/groups/:id" element={<GroupPage />} />
        <Route path="/galleries" element={<Galleries />} />
        <Route path="/galleries/:id" element={<GalleryPage />} />
        <Route path="/markers" element={<Markers />} />
        <Route path="/downloads" element={<Downloads />} />
        <Route path="/stats" element={<Stats />} />
        <Route path="/duplicates" element={<Duplicates />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)
