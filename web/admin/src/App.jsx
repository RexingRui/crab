import { Navigate, Route, Routes } from 'react-router-dom'
import Shell from './components/Shell'
import Login from './views/Login'
import Orders from './views/Orders'
import Specs from './views/Specs'
import Stats from './views/Stats'
import { getToken } from './api/client'

function Guard({ children }) {
  return getToken() ? children : <Navigate to='/login' replace />
}

export default function App() {
  return (
    <Routes>
      <Route path='/login' element={<Login />} />
      <Route
        path='/'
        element={<Guard><Shell /></Guard>}
      >
        <Route index element={<Navigate to='/orders' replace />} />
        <Route path='orders' element={<Orders />} />
        <Route path='specs' element={<Specs />} />
        <Route path='stats' element={<Stats />} />
      </Route>
      <Route path='*' element={<Navigate to='/orders' replace />} />
    </Routes>
  )
}
