import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import './styles/global.css'
import './styles/settings.css'
import { AuthProvider } from './api/AuthProvider.jsx'
import App from './App.jsx'

// Geist fonts
const preconnects = ['https://fonts.googleapis.com', 'https://fonts.gstatic.com']
preconnects.forEach(href => {
  const el = document.createElement('link')
  el.rel = 'preconnect'
  el.href = href
  if (href.includes('gstatic')) el.crossOrigin = 'anonymous'
  document.head.appendChild(el)
})
const fontLink = document.createElement('link')
fontLink.rel = 'stylesheet'
fontLink.href = 'https://fonts.googleapis.com/css2?family=Geist+Mono:wght@400;500;600&family=Geist:wght@400;500;600;700&display=swap'
document.head.appendChild(fontLink)

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <AuthProvider>
      <App />
    </AuthProvider>
  </StrictMode>,
)
